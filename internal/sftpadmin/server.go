package sftpadmin

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

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
	s.mux.HandleFunc("POST /users/delete-key", s.handleDeleteKey)
	s.mux.HandleFunc("POST /users/key-note", s.handleKeyNote)
	s.mux.HandleFunc("POST /users/remove-invalid-keys", s.handleRemoveInvalidKeys)
	s.mux.HandleFunc("POST /users/generate-key", s.handleGenerateKey)
	s.mux.HandleFunc("POST /users/status", s.handleStatus)
	s.mux.HandleFunc("POST /users/delete", s.handleDelete)
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
		s.dashboardWithError(w, sess.csrf, "Could not load users", "cannot load users", http.StatusInternalServerError)
		return
	}
	v := s.dashboardView(m, sess.csrf)
	v.Notice = repairNotice(r)
	renderUsers(w, v)
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
	sess, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	username := strings.TrimSpace(r.FormValue("username"))
	if err := ValidateUsername(username); err != nil {
		s.dashboardWithError(w, sess.csrf, "Could not create user", err.Error(), http.StatusBadRequest)
		return
	}
	pasted := strings.TrimSpace(r.FormValue("public_key"))
	var pub, priv string
	if pasted == "" {
		kp, err := s.gen(username)
		if err != nil {
			s.dashboardWithError(w, sess.csrf, "Could not create user", "key generation failed", http.StatusInternalServerError)
			return
		}
		canon, _, err := ValidatePublicKey(kp.PublicKey)
		if err != nil {
			s.dashboardWithError(w, sess.csrf, "Could not create user", "generated key failed validation", http.StatusInternalServerError)
			return
		}
		pub, priv = canon, kp.PrivatePEM
	} else {
		canon, _, err := ValidatePublicKey(pasted)
		if err != nil {
			s.dashboardWithError(w, sess.csrf, "Could not create user", err.Error(), http.StatusBadRequest)
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
		s.dashboardWithError(w, sess.csrf, "Could not create user", err.Error(), http.StatusConflict)
		return
	}
	if priv == "" {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	token, err := newToken()
	if err != nil {
		s.dashboardWithError(w, sess.csrf, "Could not create user", "download token creation failed", http.StatusInternalServerError)
		return
	}
	s.mu.Lock()
	s.pending[token] = pendingKey{username: username, private: priv, expires: time.Now().Add(pendingKeyTTL)}
	s.mu.Unlock()
	_, fp, _ := ValidatePublicKey(pub)
	s.dashboardWithGenerated(w, sess.csrf, username, token, fp)
}

// dashboardWithGenerated reloads the manifest and renders the users
// dashboard with the shared one-time Generated result card. Both generate
// paths (create-user with an empty key, existing-user row generation)
// funnel here so the overlay markup is identical. Plain GET / leaves
// Generated nil so no card renders; following the Close anchor back to /
// clears it with zero JavaScript.
func (s *Server) dashboardWithGenerated(w http.ResponseWriter, csrf, username, token, fp string) {
	m, err := s.store.Load()
	if err != nil {
		http.Error(w, "cannot load users", http.StatusInternalServerError)
		return
	}
	v := s.dashboardView(m, csrf)
	v.Generated = &generatedResult{Username: username, Token: token, Fingerprint: fp}
	renderUsers(w, v)
}

func (s *Server) dashboardWithError(w http.ResponseWriter, csrf, title, message string, status int) {
	m, err := s.store.Load()
	if err != nil {
		http.Error(w, "cannot load users", http.StatusInternalServerError)
		return
	}
	v := s.dashboardView(m, csrf)
	v.ErrorTitle = title
	v.Error = message
	renderUsersStatus(w, v, status)
}

func (s *Server) dashboardView(m Manifest, csrf string) usersView {
	v := userDashboard(m, csrf)
	if notes, err := s.store.LoadNotes(); err == nil {
		for i := range v.Users {
			for j := range v.Users[i].Keys {
				if v.Users[i].Keys[j].Valid {
					v.Users[i].Keys[j].Note = notes[keyNoteID(v.Users[i].Username, v.Users[i].Keys[j].Fingerprint)]
				}
			}
		}
	}
	v.Problems = invalidProblems(m)
	return v
}

// errNoSuchUser and errNoSuchKey let mutation closures report lookups that
// pre-checks already resolved; handlers map them to 404 even on races.
var (
	errNoSuchUser = errors.New("user not found")
	errNoSuchKey  = errors.New("key not found")
)

// writeError maps a Store.Update failure to its response contract: unknown
// users or keys are 404, strict-manifest conflicts needing dedicated repair
// are 409, anything else (including persistence failures) is 500.
func writeError(w http.ResponseWriter, err error) {
	http.Error(w, err.Error(), statusForError(err))
}

func statusForError(err error) int {
	switch {
	case errors.Is(err, errNoSuchUser) || errors.Is(err, errNoSuchKey):
		return http.StatusNotFound
	case errors.Is(err, ErrManifestInvalid):
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}

// usableKeys counts entries that pass canonical validation.
func usableKeys(keys []string) int {
	n := 0
	for _, k := range keys {
		if _, _, err := ValidatePublicKey(k); err == nil {
			n++
		}
	}
	return n
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
		return errNoSuchUser
	}); err != nil {
		writeError(w, err)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// handleDeleteKey removes one valid key identified by canonical fingerprint,
// leaving all other keys untouched. Removing the final usable key disables
// the user with a valid empty key set so reconciliation revokes access.
func (s *Server) handleDeleteKey(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	username := strings.TrimSpace(r.FormValue("username"))
	fp := strings.TrimSpace(r.FormValue("fingerprint"))
	if err := ValidateUsername(username); err != nil {
		s.dashboardWithError(w, sess.csrf, "Could not remove key", err.Error(), http.StatusBadRequest)
		return
	}
	if !ValidFingerprint(fp) {
		s.dashboardWithError(w, sess.csrf, "Could not remove key", "invalid fingerprint: want canonical SHA256 form", http.StatusBadRequest)
		return
	}
	m, err := s.store.Load()
	if err != nil {
		s.dashboardWithError(w, sess.csrf, "Could not remove key", "cannot load users", http.StatusInternalServerError)
		return
	}
	switch userFound, keyFound := findKey(m, username, fp); {
	case !userFound:
		s.dashboardWithError(w, sess.csrf, "Could not remove key", errNoSuchUser.Error(), http.StatusNotFound)
		return
	case !keyFound:
		s.dashboardWithError(w, sess.csrf, "Could not remove key", errNoSuchKey.Error(), http.StatusNotFound)
		return
	}
	if err := s.store.Update(func(m *Manifest) error {
		return deleteKey(m, username, fp)
	}); err != nil {
		s.dashboardWithError(w, sess.csrf, "Could not remove key", err.Error(), statusForError(err))
		return
	}
	// Best effort: a removed key must not leave an orphan sidecar note.
	// A prune failure never fails the delete itself.
	_ = s.store.DeleteNote(username, fp)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// findKey reports whether the manifest holds the user and whether one of
// the user's valid keys carries fp. Invalid entries have no fingerprint
// and can never match.
func findKey(m Manifest, username, fp string) (userFound, keyFound bool) {
	for _, u := range m.Users {
		if u.Username != username {
			continue
		}
		userFound = true
		for _, k := range u.AuthorizedKeys {
			if kfp, err := FingerprintOf(k); err == nil && kfp == fp {
				keyFound = true
			}
		}
	}
	return userFound, keyFound
}

// deleteKey applies fingerprint-scoped removal inside a store mutation:
// only the matching valid key is dropped, order is preserved, and a user
// left with no usable keys is disabled with a valid empty key set.
func deleteKey(m *Manifest, username, fp string) error {
	for i := range m.Users {
		if m.Users[i].Username != username {
			continue
		}
		kept := make([]string, 0, len(m.Users[i].AuthorizedKeys))
		matched := false
		for _, k := range m.Users[i].AuthorizedKeys {
			if kfp, err := FingerprintOf(k); err == nil && kfp == fp && !matched {
				matched = true
				continue
			}
			kept = append(kept, k)
		}
		if !matched {
			return errNoSuchKey
		}
		m.Users[i].AuthorizedKeys = kept
		if m.Users[i].Enabled && usableKeys(kept) == 0 {
			m.Users[i].Enabled = false
			m.Users[i].AuthorizedKeys = []string{}
		}
		return nil
	}
	return errNoSuchUser
}

// handleKeyNote stores a display-only operator note for one valid key.
// Notes live in a sidecar file, never affect validation or fingerprints,
// and are never logged. An empty note clears the entry.
func (s *Server) handleKeyNote(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	username := strings.TrimSpace(r.FormValue("username"))
	fp := strings.TrimSpace(r.FormValue("fingerprint"))
	note := strings.TrimSpace(r.FormValue("note"))
	if err := ValidateUsername(username); err != nil {
		s.dashboardWithError(w, sess.csrf, "Could not save key note", err.Error(), http.StatusBadRequest)
		return
	}
	if !ValidFingerprint(fp) {
		s.dashboardWithError(w, sess.csrf, "Could not save key note", "invalid fingerprint: want canonical SHA256 form", http.StatusBadRequest)
		return
	}
	if utf8.RuneCountInString(note) > MaxKeyNoteLength {
		s.dashboardWithError(w, sess.csrf, "Could not save key note", "note too long: max 120 characters", http.StatusBadRequest)
		return
	}
	m, err := s.store.Load()
	if err != nil {
		s.dashboardWithError(w, sess.csrf, "Could not save key note", "cannot load users", http.StatusInternalServerError)
		return
	}
	switch userFound, keyFound := findKey(m, username, fp); {
	case !userFound:
		s.dashboardWithError(w, sess.csrf, "Could not save key note", errNoSuchUser.Error(), http.StatusNotFound)
		return
	case !keyFound:
		s.dashboardWithError(w, sess.csrf, "Could not save key note", errNoSuchKey.Error(), http.StatusNotFound)
		return
	}
	if err := s.store.SetNote(username, fp, note); err != nil {
		s.dashboardWithError(w, sess.csrf, "Could not save key note", "cannot save note", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// repairNotice renders the post-repair result from strict query params:
// repaired must be a non-negative integer, disabled a comma list of valid
// usernames. Anything malformed is ignored and yields no notice; rendering
// goes through html/template so values are escaped.
func repairNotice(r *http.Request) string {
	q := r.URL.Query()
	raw, ok := q["repaired"]
	if !ok || len(raw) == 0 || raw[0] == "" {
		return ""
	}
	n, err := strconv.Atoi(raw[0])
	if err != nil || n < 0 {
		return ""
	}
	if n == 0 {
		return "No invalid entries found."
	}
	var disabled []string
	for _, name := range strings.Split(q.Get("disabled"), ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if ValidateUsername(name) != nil {
			continue
		}
		disabled = append(disabled, name)
	}
	word := "entries"
	if n == 1 {
		word = "entry"
	}
	if len(disabled) == 0 {
		return fmt.Sprintf("Removed %d invalid %s; disabled: none.", n, word)
	}
	return fmt.Sprintf("Removed %d invalid %s; disabled: %s.", n, word, strings.Join(disabled, ", "))
}

// handleRemoveInvalidKeys runs manifest-wide invalid-key repair: every
// entry failing canonical validation is dropped, valid keys and unrelated
// fields are preserved, and users left with zero valid keys are disabled.
func (s *Server) handleRemoveInvalidKeys(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	rep, err := s.store.RemoveInvalidKeys()
	if err != nil {
		s.dashboardWithError(w, sess.csrf, "Could not repair keys", "repair failed", http.StatusInternalServerError)
		return
	}
	if m, err := s.store.Load(); err == nil {
		keep := map[string]bool{}
		for _, u := range m.Users {
			for _, k := range u.AuthorizedKeys {
				if fp, ferr := FingerprintOf(k); ferr == nil {
					keep[keyNoteID(u.Username, fp)] = true
				}
			}
		}
		_ = s.store.PruneNotes(keep)
	}
	target := "/?repaired=" + strconv.Itoa(rep.RemovedKeys)
	if len(rep.DisabledUsers) > 0 {
		target += "&disabled=" + url.QueryEscape(strings.Join(rep.DisabledUsers, ","))
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

// handleGenerateKey appends a fresh Ed25519 public key for an existing user
// and renders the dashboard with a one-time download result card.
// The user's enabled flag is never changed: disabled users stay disabled.
func (s *Server) handleGenerateKey(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	username := strings.TrimSpace(r.FormValue("username"))
	if err := ValidateUsername(username); err != nil {
		s.dashboardWithError(w, sess.csrf, "Could not generate key", err.Error(), http.StatusBadRequest)
		return
	}
	m, err := s.store.Load()
	if err != nil {
		s.dashboardWithError(w, sess.csrf, "Could not generate key", "cannot load users", http.StatusInternalServerError)
		return
	}
	known := false
	for _, u := range m.Users {
		if u.Username == username {
			known = true
		}
	}
	if !known {
		s.dashboardWithError(w, sess.csrf, "Could not generate key", errNoSuchUser.Error(), http.StatusNotFound)
		return
	}
	kp, err := s.gen(username)
	if err != nil {
		s.dashboardWithError(w, sess.csrf, "Could not generate key", "key generation failed", http.StatusInternalServerError)
		return
	}
	canon, _, err := ValidatePublicKey(kp.PublicKey)
	if err != nil {
		s.dashboardWithError(w, sess.csrf, "Could not generate key", "generated key failed validation", http.StatusInternalServerError)
		return
	}
	if err := s.store.Update(func(m *Manifest) error {
		for i := range m.Users {
			if m.Users[i].Username != username {
				continue
			}
			m.Users[i].AuthorizedKeys = append(m.Users[i].AuthorizedKeys, canon)
			return nil
		}
		return errNoSuchUser
	}); err != nil {
		s.dashboardWithError(w, sess.csrf, "Could not generate key", err.Error(), statusForError(err))
		return
	}
	token, err := newToken()
	if err != nil {
		s.dashboardWithError(w, sess.csrf, "Could not generate key", "download token creation failed", http.StatusInternalServerError)
		return
	}
	s.mu.Lock()
	s.pending[token] = pendingKey{username: username, private: kp.PrivatePEM, expires: time.Now().Add(pendingKeyTTL)}
	s.mu.Unlock()
	fp, _ := FingerprintOf(canon)
	s.dashboardWithGenerated(w, sess.csrf, username, token, fp)
}

// handleStatus flips the enabled switch (action=disable|enable), which the
// SFTP reconciler turns into key removal or restoration without restart.
// Disabling retains manifest keys; enabling needs at least one usable key.
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.requireAuth(w, r)
	if !ok {
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
		s.dashboardWithError(w, sess.csrf, "Could not update user status", "unknown action", http.StatusBadRequest)
		return
	}
	if enabled {
		m, err := s.store.Load()
		if err != nil {
			s.dashboardWithError(w, sess.csrf, "Could not update user status", "cannot load users", http.StatusInternalServerError)
			return
		}
		known, usable := false, 0
		for _, u := range m.Users {
			if u.Username == username {
				known, usable = true, usableKeys(u.AuthorizedKeys)
			}
		}
		if !known {
			s.dashboardWithError(w, sess.csrf, "Could not update user status", errNoSuchUser.Error(), http.StatusNotFound)
			return
		}
		if usable == 0 {
			s.dashboardWithError(w, sess.csrf, "Could not update user status", "cannot enable a user with no usable keys", http.StatusConflict)
			return
		}
	}
	if err := s.store.Update(func(m *Manifest) error {
		for i := range m.Users {
			if m.Users[i].Username == username {
				m.Users[i].Enabled = enabled
				return nil
			}
		}
		return errNoSuchUser
	}); err != nil {
		s.dashboardWithError(w, sess.csrf, "Could not update user status", err.Error(), statusForError(err))
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// errDeleteEnabled guards account deletion: only disabled users may be
// deleted. The server enforces this even though the UI only renders the
// Delete control on disabled rows — the UI is never trusted.
var errDeleteEnabled = errors.New("disable the user before deletion")

// handleDelete permanently removes a disabled user's whole manifest entry
// (keys included) plus its sidecar notes. The entry removal is atomic via
// Store.Update; notes are pruned afterwards with the existing PruneNotes
// over the surviving key set, so the deleted user's notes fall out with any
// other orphans. Absent users lose effective keys without any reconciler
// change (the reconciler only installs keys for listed users). Like
// disable, deletion leaves the per-user data directory on disk.
func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	username := strings.TrimSpace(r.FormValue("username"))
	if err := ValidateUsername(username); err != nil {
		s.dashboardWithError(w, sess.csrf, "Could not delete user", err.Error(), http.StatusBadRequest)
		return
	}
	m, err := s.store.Load()
	if err != nil {
		s.dashboardWithError(w, sess.csrf, "Could not delete user", "cannot load users", http.StatusInternalServerError)
		return
	}
	found, enabled := false, false
	for _, u := range m.Users {
		if u.Username == username {
			found, enabled = true, u.Enabled
		}
	}
	if !found {
		s.dashboardWithError(w, sess.csrf, "Could not delete user", errNoSuchUser.Error(), http.StatusNotFound)
		return
	}
	if enabled {
		s.dashboardWithError(w, sess.csrf, "Could not delete user", errDeleteEnabled.Error(), http.StatusConflict)
		return
	}
	if err := s.store.Update(func(m *Manifest) error {
		for i := range m.Users {
			if m.Users[i].Username != username {
				continue
			}
			if m.Users[i].Enabled {
				return errDeleteEnabled
			}
			m.Users = append(m.Users[:i], m.Users[i+1:]...)
			return nil
		}
		return errNoSuchUser
	}); err != nil {
		if errors.Is(err, errDeleteEnabled) {
			s.dashboardWithError(w, sess.csrf, "Could not delete user", err.Error(), http.StatusConflict)
			return
		}
		s.dashboardWithError(w, sess.csrf, "Could not delete user", err.Error(), statusForError(err))
		return
	}
	// Best effort: the deleted user's notes must not linger as orphans.
	// A prune failure never fails the delete itself.
	if m, err := s.store.Load(); err == nil {
		keep := map[string]bool{}
		for _, u := range m.Users {
			for _, k := range u.AuthorizedKeys {
				if fp, ferr := FingerprintOf(k); ferr == nil {
					keep[keyNoteID(u.Username, fp)] = true
				}
			}
		}
		_ = s.store.PruneNotes(keep)
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
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Disposition", `attachment; filename="`+pk.username+`_ed25519"`)
	_, _ = w.Write([]byte(pk.private))
}
