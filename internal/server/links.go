package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"time"

	"simple-fileurl/internal/config"
	"simple-fileurl/internal/links"
	"simple-fileurl/internal/store"
)

// errInvalidLinkInput marks client-fixable link request errors.
var errInvalidLinkInput = errors.New("invalid link request")

// checkBearer compares the request's Bearer token with the given admin
// token in constant time.
func checkBearer(r *http.Request, token string) bool {
	const prefix = "Bearer "
	h := r.Header.Get("Authorization")
	if token == "" || !strings.HasPrefix(h, prefix) {
		return false
	}
	got := strings.TrimPrefix(h, prefix)
	if len(got) != len(token) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(token)) == 1
}

func (s *Server) requireBearer(w http.ResponseWriter, r *http.Request) bool {
	if checkBearer(r, s.effectiveConfig().AdminToken) {
		return true
	}
	http.Error(w, "unauthorized", http.StatusUnauthorized)
	return false
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// linkJSON is the wire form of a link. Password hashes are never included.
type linkJSON struct {
	ID          string      `json:"id"`
	Scope       links.Scope `json:"scope"`
	HasPassword bool        `json:"has_password"`
	CreatedAt   time.Time   `json:"created_at"`
	ExpiresAt   *time.Time  `json:"expires_at,omitempty"`
	CreatedBy   string      `json:"created_by,omitempty"`
	Description string      `json:"description,omitempty"`
	URL         string      `json:"url,omitempty"`
}

func linkSummary(l links.Link) linkJSON {
	return linkJSON{
		ID:          l.ID,
		Scope:       l.Scope,
		HasPassword: l.HasPassword(),
		CreatedAt:   l.CreatedAt,
		ExpiresAt:   l.ExpiresAt,
		Description: l.Description,
	}
}

func linkDetail(l links.Link) linkJSON {
	j := linkSummary(l)
	j.CreatedBy = l.CreatedBy
	return j
}

func (s *Server) linkURL(id string) string {
	return strings.TrimRight(s.effectiveConfig().PublicURL, "/") + "/l/" + id
}

type createLinkRequest struct {
	Password    string      `json:"password"`
	Scope       links.Scope `json:"scope"`
	ExpiresAt   *time.Time  `json:"expires_at"`
	Description string      `json:"description"`
}

func checkLinkPassword(password string) error {
	if len(password) > 72 {
		return errInvalidLinkInput
	}
	return nil
}

func (s *Server) handleLinksCreate(w http.ResponseWriter, r *http.Request) {
	if !s.requireBearer(w, r) {
		return
	}
	var req createLinkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if err := req.Scope.Validate(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.ExpiresAt != nil {
		if !req.ExpiresAt.After(time.Now()) {
			http.Error(w, "expires_at must be in the future", http.StatusBadRequest)
			return
		}
		utc := req.ExpiresAt.UTC()
		req.ExpiresAt = &utc
	}
	if err := checkLinkPassword(req.Password); err != nil {
		http.Error(w, "password too long", http.StatusBadRequest)
		return
	}
	var hash string
	if req.Password != "" {
		h, err := links.HashPassword(req.Password)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		hash = h
	}
	var l links.Link
	created := false
	for i := 0; i < 3 && !created; i++ {
		id, err := links.NewID()
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		cand := links.Link{
			ID:           id,
			PasswordHash: hash,
			Scope:        req.Scope,
			CreatedAt:    time.Now().UTC().Truncate(time.Second),
			ExpiresAt:    req.ExpiresAt,
			CreatedBy:    "admin",
			Description:  req.Description,
		}
		if err := s.links.Create(cand); err == nil {
			l, created = cand, true
		} else if !errors.Is(err, links.ErrExists) {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
	}
	if !created {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	j := linkDetail(l)
	j.URL = s.linkURL(l.ID)
	writeJSON(w, http.StatusCreated, j)
}

func (s *Server) handleLinksList(w http.ResponseWriter, r *http.Request) {
	if !s.requireBearer(w, r) {
		return
	}
	stored := s.links.List()
	out := make([]linkJSON, 0, len(stored))
	for _, l := range stored {
		out = append(out, linkSummary(l))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleLinkGet(w http.ResponseWriter, r *http.Request) {
	if !s.requireBearer(w, r) {
		return
	}
	l, ok := s.links.Get(r.PathValue("id"))
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, linkDetail(l))
}

type patchLinkRequest struct {
	Password    *string `json:"password"`
	ExpiresAt   *string `json:"expires_at"`
	Description *string `json:"description"`
}

func (s *Server) handleLinkPatch(w http.ResponseWriter, r *http.Request) {
	if !s.requireBearer(w, r) {
		return
	}
	var req patchLinkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	id := r.PathValue("id")
	err := s.links.Update(id, func(l *links.Link) error {
		if req.Password != nil {
			if *req.Password == "" {
				l.PasswordHash = ""
			} else {
				if err := checkLinkPassword(*req.Password); err != nil {
					return err
				}
				h, err := links.HashPassword(*req.Password)
				if err != nil {
					return err
				}
				l.PasswordHash = h
			}
		}
		if req.ExpiresAt != nil {
			if *req.ExpiresAt == "" {
				l.ExpiresAt = nil
			} else {
				t, err := time.Parse(time.RFC3339, *req.ExpiresAt)
				if err != nil || !t.After(time.Now()) {
					return errInvalidLinkInput
				}
				t = t.UTC()
				l.ExpiresAt = &t
			}
		}
		if req.Description != nil {
			l.Description = *req.Description
		}
		return nil
	})
	switch {
	case errors.Is(err, links.ErrNotFound):
		http.Error(w, "not found", http.StatusNotFound)
	case errors.Is(err, errInvalidLinkInput):
		http.Error(w, "invalid link update", http.StatusBadRequest)
	case err != nil:
		http.Error(w, "internal error", http.StatusInternalServerError)
	default:
		l, ok := s.links.Get(id)
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		writeJSON(w, http.StatusOK, linkDetail(l))
	}
}

func (s *Server) handleLinkDelete(w http.ResponseWriter, r *http.Request) {
	if !s.requireBearer(w, r) {
		return
	}
	if err := s.links.Delete(r.PathValue("id")); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// sharedTop is the first segment of the shared prefix (e.g. "files").
func sharedTop(cfg config.Config) string {
	prefix, err := config.NormalizePrefix(cfg.SharePrefix)
	if err != nil {
		return ""
	}
	return strings.Split(prefix, "/")[0]
}

// filterByScope keeps entries visible to the link: admin sees everything,
// a user link sees the shared prefix plus that user's directory.
func filterByScope(cfg config.Config, entries []store.Entry, scope links.Scope) []store.Entry {
	if scope.Type == links.ScopeAdmin {
		return entries
	}
	top := sharedTop(cfg)
	var out []store.Entry
	for _, e := range entries {
		seg, _, _ := strings.Cut(e.LogicalPath, "/")
		if seg == top || seg == scope.User {
			out = append(out, e)
		}
	}
	return out
}

// linkSessionCookieName scopes the session cookie to one link.
func linkSessionCookieName(id string) string {
	return "fl_" + id
}

// signLinkSession builds an HMAC-signed session token for a link.
func signLinkSession(id string, exp int64, key string) string {
	msg := id + "." + strconv.FormatInt(exp, 10)
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(msg))
	return base64.RawURLEncoding.EncodeToString([]byte(msg)) + "." +
		base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// verifyLinkSession checks the cookie against the link ID and key.
func verifyLinkSession(cookie, id, key string) bool {
	if key == "" {
		return false
	}
	parts := strings.Split(cookie, ".")
	if len(parts) != 2 {
		return false
	}
	msgBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return false
	}
	msg := string(msgBytes)
	linkID, expStr, _ := strings.Cut(msg, ".")
	if linkID != id {
		return false
	}
	exp, err := strconv.ParseInt(expStr, 10, 64)
	if err != nil || time.Now().Unix() > exp {
		return false
	}
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(msg))
	want, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return false
	}
	return hmac.Equal(mac.Sum(nil), want)
}

// linkSessionOK reports whether the request may view the link: open links
// always pass, protected links need a valid session cookie.
func linkSessionOK(cfg config.Config, r *http.Request, l links.Link) bool {
	if !l.HasPassword() {
		return true
	}
	c, err := r.Cookie(linkSessionCookieName(l.ID))
	if err != nil {
		return false
	}
	return verifyLinkSession(c.Value, l.ID, cfg.AdminToken)
}

var linkListTemplate = template.Must(template.New("linklist").Parse(`<!DOCTYPE html>
<html lang="en">
<head><meta charset="utf-8"><title>Shared files</title></head>
<body>
<h1>Shared files</h1>
{{if .Description}}<p>{{.Description}}</p>{{end}}
{{if .Entries}}
<ul>
{{range .Entries}}<li>{{.LogicalPath}} ({{.Size}} bytes)<br><a href="{{.URL}}">{{.URL}}</a></li>
{{end}}</ul>
{{else}}
<p>No files available.</p>
{{end}}
</body>
</html>`))

var linkPasswordTemplate = template.Must(template.New("linkpassword").Parse(`<!DOCTYPE html>
<html lang="en">
<head><meta charset="utf-8"><title>Password required</title></head>
<body>
<h1>Password required</h1>
<form method="post" action="/l/{{.ID}}/auth">
<input type="password" name="password" autocomplete="current-password">
<button type="submit">Show files</button>
</form>
</body>
</html>`))

type linkEntry struct {
	LogicalPath string
	Size        int64
	URL         string
}

type linkPageData struct {
	Description string
	Entries     []linkEntry
}

func (s *Server) handleLinkPage(w http.ResponseWriter, r *http.Request) {
	l, ok := s.links.Get(r.PathValue("id"))
	if !ok || l.Expired(time.Now()) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	cfg := s.effectiveConfig()
	if !linkSessionOK(cfg, r, l) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusUnauthorized)
		_ = linkPasswordTemplate.Execute(w, struct{ ID string }{ID: l.ID})
		return
	}
	entries, err := s.store.ListWith(cfg)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	data := linkPageData{Description: l.Description}
	for _, e := range filterByScope(cfg, entries, l.Scope) {
		data.Entries = append(data.Entries, linkEntry{
			LogicalPath: e.LogicalPath,
			Size:        e.Size,
			URL:         cfg.ShareURL(e.DirHash, e.FileHash),
		})
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := linkListTemplate.Execute(w, data); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

func (s *Server) handleLinkAuth(w http.ResponseWriter, r *http.Request) {
	l, ok := s.links.Get(r.PathValue("id"))
	if !ok || l.Expired(time.Now()) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if !l.HasPassword() {
		http.Redirect(w, r, "/l/"+l.ID, http.StatusFound)
		return
	}
	if !links.CheckPassword(l.PasswordHash, r.FormValue("password")) {
		http.Error(w, "invalid password", http.StatusUnauthorized)
		return
	}
	exp := time.Now().Add(24 * time.Hour)
	http.SetCookie(w, &http.Cookie{
		Name:     linkSessionCookieName(l.ID),
		Value:    signLinkSession(l.ID, exp.Unix(), s.effectiveConfig().AdminToken),
		Path:     "/l/" + l.ID,
		Expires:  exp,
		MaxAge:   24 * 60 * 60,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, "/l/"+l.ID, http.StatusFound)
}

type linkFileJSON struct {
	LogicalPath string `json:"logical_path"`
	LogicalDir  string `json:"logical_dir"`
	DirHash     string `json:"dir_hash"`
	FileHash    string `json:"file_hash"`
	Size        int64  `json:"size"`
	URL         string `json:"url"`
}

type linkFilesResponse struct {
	LinkID string         `json:"link_id"`
	Scope  links.Scope    `json:"scope"`
	Files  []linkFileJSON `json:"files"`
}

func (s *Server) handleLinkFiles(w http.ResponseWriter, r *http.Request) {
	l, ok := s.links.Get(r.PathValue("id"))
	if !ok || l.Expired(time.Now()) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	cfg := s.effectiveConfig()
	if !linkSessionOK(cfg, r, l) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	entries, err := s.store.ListWith(cfg)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	resp := linkFilesResponse{LinkID: l.ID, Scope: l.Scope, Files: []linkFileJSON{}}
	for _, e := range filterByScope(cfg, entries, l.Scope) {
		resp.Files = append(resp.Files, linkFileJSON{
			LogicalPath: e.LogicalPath,
			LogicalDir:  e.LogicalDir,
			DirHash:     e.DirHash,
			FileHash:    e.FileHash,
			Size:        e.Size,
			URL:         cfg.ShareURL(e.DirHash, e.FileHash),
		})
	}
	writeJSON(w, http.StatusOK, resp)
}
