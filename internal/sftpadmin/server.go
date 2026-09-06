package sftpadmin

import (
	"errors"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"simple-fileurl/internal/config"
)

// pendingKeyTTL bounds how long a generated private key waits for its
// single download before it is discarded from memory.
const pendingKeyTTL = 10 * time.Minute

// pendingKey is a generated private key awaiting its one and only download.
// It lives in process memory: never in the manifest, never on disk, never
// in logs.
type pendingKey struct {
	username string
	private  string
	expires  time.Time
}

// Server serves the admin UI and its HTTP handlers.
type Server struct {
	store *Store
	auth  *authState
	// password is the startup admin password; the shared config file
	// overrides it at login time when present.
	password string
	// sharedPath is the shared config.json location.
	sharedPath string
	// gen creates keypairs; replaceable in tests for determinism.
	gen     func(comment string) (KeyPair, error)
	mu      sync.Mutex
	pending map[string]pendingKey
	mux     *http.ServeMux
}

// NewServer wires routes. cfg.Password must already be validated non-empty.
func NewServer(cfg Config, st *Store) *Server {
	s := &Server{
		store:      st,
		auth:       newAuthState(cfg.Password),
		password:   cfg.Password,
		sharedPath: cfg.SharedPath,
		gen:        GenerateEd25519KeyPair,
		pending:    map[string]pendingKey{},
		mux:        http.NewServeMux(),
	}
	s.mux.HandleFunc("GET /healthz", s.handleHealth)
	s.mux.HandleFunc("GET /{$}", s.handleIndex)
	s.mux.HandleFunc("GET /login", s.handleLoginPage)
	s.mux.HandleFunc("POST /login", s.handleLogin)
	s.mux.HandleFunc("POST /logout", s.handleLogout)
	s.mux.HandleFunc("POST /users/create", s.handleCreate)
	s.mux.HandleFunc("POST /users/add-key", s.handleAddKey)
	s.mux.HandleFunc("POST /users/status", s.handleStatus)
	s.mux.HandleFunc("GET /keys/download", s.handleDownload)
	s.mux.HandleFunc("GET /settings", s.handleSettings)
	s.mux.HandleFunc("POST /settings", s.handleSettingsSave)
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

// current resolves the request session, if any.
func (s *Server) current(r *http.Request) (session, bool) {
	c, err := r.Cookie(sessionCookieName)
	if err != nil {
		return session{}, false
	}
	return s.auth.lookup(c.Value)
}

// requireAuth rejects unauthenticated management access: browser navigations
// go back to the password prompt, direct POSTs get 401 with no data.
func (s *Server) requireAuth(w http.ResponseWriter, r *http.Request) (session, bool) {
	sess, ok := s.current(r)
	if !ok {
		if r.Method == http.MethodGet {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
		} else {
			http.Error(w, "login required", http.StatusUnauthorized)
		}
		return session{}, false
	}
	if r.Method == http.MethodPost && !checkCSRF(sess, r.FormValue(csrfField)) {
		http.Error(w, "bad csrf token", http.StatusForbidden)
		return session{}, false
	}
	return sess, true
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	m, err := s.store.Load()
	if err != nil {
		http.Error(w, "cannot load users", http.StatusInternalServerError)
		return
	}
	renderUsers(w, userDashboard(m, sess.csrf))
}

func (s *Server) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.current(r); ok {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	renderLogin(w, "")
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if !s.auth.loginAllowed(ip) {
		http.Error(w, "too many failed attempts, try again later", http.StatusTooManyRequests)
		return
	}
	if !s.checkLoginPassword(r.FormValue("password")) {
		s.auth.recordFailure(ip)
		renderLogin(w, "Wrong password.")
		return
	}
	s.auth.recordSuccess(ip)
	id, _, err := s.auth.createSession()
	if err != nil {
		http.Error(w, "session creation failed", http.StatusInternalServerError)
		return
	}
	setSessionCookie(w, id)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// checkLoginPassword compares the candidate against the shared config
// password when present, falling back to the startup password.
func (s *Server) checkLoginPassword(got string) bool {
	want := s.password
	if sc, err := config.LoadShared(s.sharedPath); err == nil && sc.SftpAdminPassword != "" {
		want = sc.SftpAdminPassword
	}
	return checkPasswordAgainst(got, want)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookieName); err == nil {
		if sess, ok := s.auth.lookup(c.Value); ok && !checkCSRF(sess, r.FormValue(csrfField)) {
			http.Error(w, "bad csrf token", http.StatusForbidden)
			return
		}
		s.auth.destroy(c.Value)
	}
	clearSessionCookie(w)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// handleCreate adds a user with either a pasted public key or a freshly
// generated ed25519 pair. Generated private keys are stashed in memory for
// one download and never persisted.
func (s *Server) handleCreate(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	username := strings.TrimSpace(r.FormValue("username"))
	if err := ValidateUsername(username); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	pasted := strings.TrimSpace(r.FormValue("public_key"))
	var pub, priv string
	if pasted == "" {
		kp, err := s.gen(username)
		if err != nil {
			http.Error(w, "key generation failed", http.StatusInternalServerError)
			return
		}
		canon, _, err := ValidatePublicKey(kp.PublicKey)
		if err != nil {
			http.Error(w, "generated key failed validation", http.StatusInternalServerError)
			return
		}
		pub, priv = canon, kp.PrivatePEM
	} else {
		canon, _, err := ValidatePublicKey(pasted)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		pub = canon
	}
	if err := s.store.Update(func(m *Manifest) error {
		for _, u := range m.Users {
			if u.Username == username {
				return errors.New("user already exists")
			}
		}
		m.Users = append(m.Users, User{Username: username, Enabled: true, AuthorizedKeys: []string{pub}})
		return nil
	}); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	if priv == "" {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	token, err := newToken()
	if err != nil {
		http.Error(w, "download token creation failed", http.StatusInternalServerError)
		return
	}
	s.mu.Lock()
	s.pending[token] = pendingKey{username: username, private: priv, expires: time.Now().Add(pendingKeyTTL)}
	s.mu.Unlock()
	_, fp, _ := ValidatePublicKey(pub)
	renderCreated(w, createdView{Username: username, Token: token, Fingerprint: fp})
}

// handleAddKey appends an external public key idempotently: re-submitting
// the same key is a no-op success, existing keys are never replaced.
func (s *Server) handleAddKey(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	username := strings.TrimSpace(r.FormValue("username"))
	canon, _, err := ValidatePublicKey(strings.TrimSpace(r.FormValue("public_key")))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.store.Update(func(m *Manifest) error {
		for i := range m.Users {
			if m.Users[i].Username != username {
				continue
			}
			for _, k := range m.Users[i].AuthorizedKeys {
				if k == canon {
					return nil
				}
			}
			m.Users[i].AuthorizedKeys = append(m.Users[i].AuthorizedKeys, canon)
			return nil
		}
		return errors.New("user not found")
	}); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// handleStatus flips the enabled switch (action=disable|enable), which the
// SFTP reconciler turns into key removal or restoration without restart.
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	username := strings.TrimSpace(r.FormValue("username"))
	var enabled bool
	switch r.FormValue("action") {
	case "disable":
		enabled = false
	case "enable":
		enabled = true
	default:
		http.Error(w, "unknown action", http.StatusBadRequest)
		return
	}
	if err := s.store.Update(func(m *Manifest) error {
		for i := range m.Users {
			if m.Users[i].Username == username {
				m.Users[i].Enabled = enabled
				return nil
			}
		}
		return errors.New("user not found")
	}); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// sharedConfigPath resolves the shared config file location.
func (s *Server) sharedConfigPath() string {
	if s.sharedPath != "" {
		return s.sharedPath
	}
	return config.DefaultSharedConfigPath
}

// settingsGroups loads current values merged with registry metadata,
// grouped by service in registry order.
func (s *Server) settingsGroups() ([]settingsGroup, error) {
	values, err := GetSettings(s.sharedConfigPath())
	if err != nil {
		return nil, err
	}
	var groups []settingsGroup
	index := map[string]int{}
	for _, v := range values {
		i, ok := index[v.Service]
		if !ok {
			groups = append(groups, settingsGroup{Service: v.Service})
			i = len(groups) - 1
			index[v.Service] = i
		}
		groups[i].Items = append(groups[i].Items, v)
	}
	return groups, nil
}

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	groups, err := s.settingsGroups()
	if err != nil {
		http.Error(w, "cannot load settings", http.StatusInternalServerError)
		return
	}
	renderSettings(w, settingsView{pageData: pageData{CSRF: sess.csrf}, Groups: groups})
}

// handleSettingsSave validates every submitted setting and persists them
// as one transaction: secrets left empty keep their current value,
// everything else is validated, and any failure leaves the file untouched.
// Each saved change is logged with values redacted for secrets and never
// includes session or CSRF material.
func (s *Server) handleSettingsSave(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	path := s.sharedConfigPath()
	before, err := GetSettings(path)
	if err != nil {
		http.Error(w, "cannot load settings", http.StatusInternalServerError)
		return
	}
	old := map[string]string{}
	for _, v := range before {
		old[v.Key] = v.Value
	}
	vals := map[string]string{}
	for _, def := range SettingsRegistry {
		if _, present := r.Form[def.Key]; present {
			vals[def.Key] = strings.TrimSpace(r.FormValue(def.Key))
		}
	}
	errs, err := ApplySettings(path, vals)
	if err != nil {
		http.Error(w, "cannot load settings", http.StatusInternalServerError)
		return
	}
	var saved []string
	for _, def := range SettingsRegistry {
		if _, bad := errs[def.Key]; bad {
			continue
		}
		if _, submitted := vals[def.Key]; !submitted {
			continue
		}
		if vals[def.Key] == "" && def.IsSecret {
			continue
		}
		saved = append(saved, def.Key)
		display, prev := vals[def.Key], old[def.Key]
		if def.IsSecret {
			display, prev = "***", "***"
		}
		log.Printf("settings: updated %s from %q to %q", def.Key, prev, display)
	}
	groups, err := s.settingsGroups()
	if err != nil {
		http.Error(w, "cannot load settings", http.StatusInternalServerError)
		return
	}
	v := settingsView{pageData: pageData{CSRF: sess.csrf}, Groups: groups}
	if len(errs) > 0 {
		v.Errors = errs
	}
	if len(saved) > 0 {
		v.Notice = "Saved: " + strings.Join(saved, ", ")
	}
	renderSettings(w, v)
}

// handleDownload serves a generated private key exactly once: the lookup
// deletes the entry first, so a second request (or a crash-replay) finds
// nothing and gets 404.
func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	token := r.URL.Query().Get("token")
	s.mu.Lock()
	pk, ok := s.pending[token]
	if ok {
		delete(s.pending, token)
	}
	s.mu.Unlock()
	if !ok || token == "" || time.Now().After(pk.expires) {
		http.Error(w, "key already downloaded or expired", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="`+pk.username+`_ed25519"`)
	_, _ = w.Write([]byte(pk.private))
}
