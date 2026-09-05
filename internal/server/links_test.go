package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"simple-fileurl/internal/config"
	"simple-fileurl/internal/links"
	"simple-fileurl/internal/store"
)

func testLinkServer(t *testing.T) (*Server, string) {
	t.Helper()
	cfg := config.Config{
		ContainerRoot: t.TempDir(),
		SharePrefix:   "files",
		PublicURL:     "https://example.test",
		HashTarget:    "file",
		HashAlgorithm: "md5",
		Port:          "8080",
		AdminToken:    "test-token",
	}
	ls := links.NewStore(t.TempDir())
	if err := ls.Load(); err != nil {
		t.Fatal(err)
	}
	return New(cfg, store.New(cfg), ls), "test-token"
}

func apiRequest(t *testing.T, method, target, token, body string) *http.Request {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, target, nil)
	} else {
		r = httptest.NewRequest(method, target, strings.NewReader(body))
	}
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	return r
}

func decodeLink(t *testing.T, rec *httptest.ResponseRecorder) linkJSON {
	t.Helper()
	var j linkJSON
	if err := json.NewDecoder(rec.Body).Decode(&j); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return j
}

func TestLinksCreateAndList(t *testing.T) {
	srv, token := testLinkServer(t)

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, apiRequest(t, http.MethodPost, "/api/links",
		token, `{"password":"s3cret!","scope":{"type":"user","user":"jonathan"},"description":"J files"}`))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %q", rec.Code, rec.Body.String())
	}
	created := decodeLink(t, rec)
	if created.ID == "" || !strings.HasPrefix(created.URL, "https://example.test/l/") {
		t.Fatalf("create response: %+v", created)
	}
	if !created.HasPassword || created.CreatedBy != "admin" {
		t.Fatalf("create flags: %+v", created)
	}
	if strings.Contains(rec.Body.String(), "password_hash") {
		t.Fatal("password hash leaked in response")
	}

	// List shows the flag but never the hash.
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, apiRequest(t, http.MethodGet, "/api/links", token, ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d", rec.Code)
	}
	var listed []linkJSON
	if err := json.NewDecoder(rec.Body).Decode(&listed); err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || !listed[0].HasPassword || listed[0].ID != created.ID {
		t.Fatalf("list: %+v", listed)
	}
	if strings.Contains(rec.Body.String(), "password_hash") {
		t.Fatal("password hash leaked in list")
	}

	// No token.
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, apiRequest(t, http.MethodGet, "/api/links", "", ""))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no token: %d", rec.Code)
	}

	// Bad scope.
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, apiRequest(t, http.MethodPost, "/api/links",
		token, `{"scope":{"type":"user","user":"Root"}}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad scope: %d", rec.Code)
	}

	// Malformed JSON and past expiry.
	for _, body := range []string{`{oops`, `{"scope":{"type":"admin"},"expires_at":"2000-01-01T00:00:00Z"}`} {
		rec = httptest.NewRecorder()
		srv.ServeHTTP(rec, apiRequest(t, http.MethodPost, "/api/links", token, body))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("bad input %q: %d", body, rec.Code)
		}
	}
}

func TestLinkGetPatchDelete(t *testing.T) {
	srv, token := testLinkServer(t)

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, apiRequest(t, http.MethodPost, "/api/links",
		token, `{"scope":{"type":"admin"}}`))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d", rec.Code)
	}
	id := decodeLink(t, rec).ID

	// Unknown ID.
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, apiRequest(t, http.MethodGet, "/api/links/nope", token, ""))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown get: %d", rec.Code)
	}

	// Rotate password, set expiry and description.
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, apiRequest(t, http.MethodPatch, "/api/links/"+id,
		token, `{"password":"n3w!","expires_at":"2099-01-01T00:00:00Z","description":"d"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("patch: %d %q", rec.Code, rec.Body.String())
	}
	patched := decodeLink(t, rec)
	if !patched.HasPassword || patched.Description != "d" || patched.ExpiresAt == nil {
		t.Fatalf("patch result: %+v", patched)
	}

	// Bad patch input.
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, apiRequest(t, http.MethodPatch, "/api/links/"+id,
		token, `{"expires_at":"not-a-time"}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad patch: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, apiRequest(t, http.MethodPatch, "/api/links/nope",
		token, `{"description":"x"}`))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown patch: %d", rec.Code)
	}

	// Delete makes the link unreachable.
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, apiRequest(t, http.MethodDelete, "/api/links/"+id, token, ""))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, apiRequest(t, http.MethodGet, "/api/links/"+id, token, ""))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("get after delete: %d", rec.Code)
	}
}

func TestLinkPatchClearAndExpired(t *testing.T) {
	srv, token := testLinkServer(t)

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, apiRequest(t, http.MethodPost, "/api/links",
		token, `{"password":"s3cret!","scope":{"type":"admin"},"expires_at":"2099-01-01T00:00:00Z"}`))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d", rec.Code)
	}
	id := decodeLink(t, rec).ID

	// Clearing password and expiry removes the gate.
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, apiRequest(t, http.MethodPatch, "/api/links/"+id,
		token, `{"password":"","expires_at":""}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("clear patch: %d %q", rec.Code, rec.Body.String())
	}
	cleared := decodeLink(t, rec)
	if cleared.HasPassword || cleared.ExpiresAt != nil {
		t.Fatalf("clear result: %+v", cleared)
	}

	// Patching an expired link reports not found instead of an empty 200.
	past := time.Now().Add(-time.Hour)
	mustCreateLink(t, srv, links.Link{ID: "stale", Scope: links.Scope{Type: links.ScopeAdmin}, CreatedAt: time.Now(), ExpiresAt: &past})
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, apiRequest(t, http.MethodPatch, "/api/links/stale",
		token, `{"description":"x"}`))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("patch expired: %d %q", rec.Code, rec.Body.String())
	}
}

func testScopeServer(t *testing.T) *Server {
	t.Helper()
	ns := t.TempDir()
	for _, dir := range []string{"files/shared", "jonathan/docs", "alice/docs"} {
		if err := os.MkdirAll(filepath.Join(ns, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		"files/shared/team.txt":   "team",
		"jonathan/docs/notes.txt": "notes",
		"alice/docs/secret.txt":   "secret",
	}
	for rel, body := range files {
		if err := os.WriteFile(filepath.Join(ns, rel), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg := config.Config{
		ContainerRoot: ns,
		SharePrefix:   "files",
		PublicURL:     "https://example.test",
		HashTarget:    "file",
		HashAlgorithm: "md5",
		Port:          "8080",
		AdminToken:    "test-token",
	}
	ls := links.NewStore(t.TempDir())
	if err := ls.Load(); err != nil {
		t.Fatal(err)
	}
	return New(cfg, store.New(cfg), ls)
}

func mustCreateLink(t *testing.T, srv *Server, l links.Link) {
	t.Helper()
	if err := srv.links.Create(l); err != nil {
		t.Fatal(err)
	}
}

func TestLinkPageScopeIsolation(t *testing.T) {
	srv := testScopeServer(t)
	mustCreateLink(t, srv, links.Link{ID: "admin1", Scope: links.Scope{Type: links.ScopeAdmin}, CreatedAt: time.Now()})
	hash, err := links.HashPassword("pw")
	if err != nil {
		t.Fatal(err)
	}
	mustCreateLink(t, srv, links.Link{ID: "user1", Scope: links.Scope{Type: links.ScopeUser, User: "jonathan"}, PasswordHash: hash, CreatedAt: time.Now()})

	// Admin link renders everything without auth.
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/l/admin1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("admin page: %d", rec.Code)
	}
	for _, want := range []string{"files/shared/team.txt", "jonathan/docs/notes.txt", "alice/docs/secret.txt"} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Fatalf("admin page missing %q", want)
		}
	}

	// Protected link without cookie shows the password form.
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/l/user1", nil))
	if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), "/l/user1/auth") {
		t.Fatalf("protected page: %d %q", rec.Code, rec.Body.String())
	}

	// Wrong password is rejected.
	form := strings.NewReader("password=nope")
	req := httptest.NewRequest(http.MethodPost, "/l/user1/auth", form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password: %d", rec.Code)
	}

	// Correct password sets a session cookie and redirects.
	form = strings.NewReader("password=pw")
	req = httptest.NewRequest(http.MethodPost, "/l/user1/auth", form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("auth: %d", rec.Code)
	}
	var session *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == "fl_user1" {
			session = c
		}
	}
	if session == nil || !session.HttpOnly {
		t.Fatalf("session cookie: %+v", rec.Result().Cookies())
	}

	// Authenticated page shows files/ + jonathan/, never alice/.
	req = httptest.NewRequest(http.MethodGet, "/l/user1", nil)
	req.AddCookie(session)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("authed page: %d", rec.Code)
	}
	for _, want := range []string{"files/shared/team.txt", "jonathan/docs/notes.txt"} {
		if !strings.Contains(body, want) {
			t.Fatalf("authed page missing %q", want)
		}
	}
	if strings.Contains(body, "alice/docs/secret.txt") {
		t.Fatal("cross-user leakage in listing page")
	}

	// JSON endpoint enforces the same isolation.
	req = httptest.NewRequest(http.MethodGet, "/l/user1/files", nil)
	req.AddCookie(session)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("files json: %d", rec.Code)
	}
	var resp linkFilesResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Files) != 2 {
		t.Fatalf("files count %d: %+v", len(resp.Files), resp)
	}
	for _, f := range resp.Files {
		if !strings.HasPrefix(f.LogicalPath, "files/") && !strings.HasPrefix(f.LogicalPath, "jonathan/") {
			t.Fatalf("leaked file %q", f.LogicalPath)
		}
		if f.URL == "" || f.DirHash == "" || f.FileHash == "" {
			t.Fatalf("incomplete file entry %+v", f)
		}
	}

	// Tampered and expired cookies are rejected.
	req = httptest.NewRequest(http.MethodGet, "/l/user1/files", nil)
	req.AddCookie(&http.Cookie{Name: "fl_user1", Value: session.Value + "x"})
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("tampered cookie: %d", rec.Code)
	}
	past := signLinkSession("user1", time.Now().Add(-time.Hour).Unix(), "test-token")
	req = httptest.NewRequest(http.MethodGet, "/l/user1/files", nil)
	req.AddCookie(&http.Cookie{Name: "fl_user1", Value: past})
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expired cookie: %d", rec.Code)
	}

	// Unknown and expired links are 404.
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/l/nope", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown link: %d", rec.Code)
	}
	pastTime := time.Now().Add(-time.Hour)
	mustCreateLink(t, srv, links.Link{ID: "gone", Scope: links.Scope{Type: links.ScopeAdmin}, CreatedAt: time.Now(), ExpiresAt: &pastTime})
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/l/gone", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expired link: %d", rec.Code)
	}
}
