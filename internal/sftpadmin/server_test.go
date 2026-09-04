package sftpadmin

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

const testPassword = "correct-horse-admin"

func newTestServer(t *testing.T) *Server {
	t.Helper()
	cfg := Config{
		Password:  testPassword,
		Addr:      ":0",
		UsersFile: filepath.Join(t.TempDir(), "users.json"),
	}
	return NewServer(cfg, NewStore(cfg.UsersFile))
}

var csrfRe = regexp.MustCompile(`name="csrf_token" value="([^"]+)"`)

func scrapeCSRF(t *testing.T, body string) string {
	t.Helper()
	m := csrfRe.FindStringSubmatch(body)
	if m == nil {
		t.Fatal("no csrf token in page")
	}
	return m[1]
}

// loginAs performs a real login and returns the session cookie plus the
// dashboard CSRF token for that session.
func loginAs(t *testing.T, srv *Server) (*http.Cookie, string) {
	t.Helper()
	form := url.Values{"password": {testPassword}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("login status = %d, want 303", rec.Code)
	}
	var sess *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookieName {
			sess = c
		}
	}
	if sess == nil {
		t.Fatal("login set no session cookie")
	}
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.AddCookie(sess)
	srv.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("dashboard status = %d, want 200", rec2.Code)
	}
	return sess, scrapeCSRF(t, rec2.Body.String())
}

func authedPost(srv *Server, sess *http.Cookie, target string, form url.Values) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(sess)
	srv.ServeHTTP(rec, req)
	return rec
}

func TestLoginGateHidesData(t *testing.T) {
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), "/login") {
		t.Errorf("anon GET / = %d %q, want redirect to /login", rec.Code, rec.Header().Get("Location"))
	}
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/login", nil))
	body := rec.Body.String()
	if !strings.Contains(body, "Admin password") || strings.Contains(body, "alice") {
		t.Error("login page must show only the password prompt")
	}
}

func TestWrongPasswordRejected(t *testing.T) {
	srv := newTestServer(t)
	form := url.Values{"password": {"wrong"}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), "Wrong password") {
		t.Error("wrong password must re-render the prompt with an error")
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookieName && c.Value != "" && c.MaxAge >= 0 {
			t.Error("wrong password must not create a session")
		}
	}
}

func TestSessionCookieAttributes(t *testing.T) {
	srv := newTestServer(t)
	form := url.Values{"password": {testPassword}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.ServeHTTP(rec, req)
	setCookie := rec.Header().Get("Set-Cookie")
	for _, want := range []string{"HttpOnly", "SameSite=Strict"} {
		if !strings.Contains(setCookie, want) {
			t.Errorf("Set-Cookie %q missing %q", setCookie, want)
		}
	}
}

func TestLogoutInvalidatesSession(t *testing.T) {
	srv := newTestServer(t)
	sess, csrf := loginAs(t, srv)
	rec := authedPost(srv, sess, "/logout", url.Values{csrfField: {csrf}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("logout = %d, want 303", rec.Code)
	}
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.AddCookie(sess)
	srv.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusSeeOther {
		t.Errorf("after logout GET / = %d, want redirect to login", rec2.Code)
	}
}

func TestLogoutRejectsBadCSRF(t *testing.T) {
	srv := newTestServer(t)
	sess, _ := loginAs(t, srv)
	rec := authedPost(srv, sess, "/logout", url.Values{csrfField: {"forged"}})
	if rec.Code != http.StatusForbidden {
		t.Errorf("logout with forged csrf = %d, want 403", rec.Code)
	}
}

func TestSessionExpiry(t *testing.T) {
	srv := newTestServer(t)
	srv.auth.ttl = 40 * time.Millisecond
	sess, _ := loginAs(t, srv)
	time.Sleep(80 * time.Millisecond)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(sess)
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Errorf("expired session GET / = %d, want redirect to login", rec.Code)
	}
}

func TestCSRFEnforcedOnMutations(t *testing.T) {
	srv := newTestServer(t)
	sess, _ := loginAs(t, srv)
	rec := authedPost(srv, sess, "/users/create", url.Values{"username": {"alice"}})
	if rec.Code != http.StatusForbidden {
		t.Errorf("POST without csrf = %d, want 403", rec.Code)
	}
	m, err := srv.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Users) != 0 {
		t.Error("CSRF-rejected request must not mutate the manifest")
	}
}

func TestFailedLoginsRateLimited(t *testing.T) {
	srv := newTestServer(t)
	var last int
	for i := 0; i < maxLoginAttempts+1; i++ {
		form := url.Values{"password": {"nope"}}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.RemoteAddr = "10.0.0.9:1234"
		srv.ServeHTTP(rec, req)
		last = rec.Code
	}
	if last != http.StatusTooManyRequests {
		t.Errorf("6th rapid failure = %d, want 429", last)
	}
}

func TestConfigRequiresPassword(t *testing.T) {
	if _, err := LoadConfig(func(string) string { return "" }); err == nil {
		t.Error("LoadConfig without password = nil, want fail-fast error")
	}
	cfg, err := LoadConfig(func(k string) string {
		if k == "SFTP_ADMIN_PASSWORD" {
			return "x"
		}
		return ""
	})
	if err != nil {
		t.Fatalf("LoadConfig = %v", err)
	}
	if cfg.Addr != DefaultAddr || cfg.UsersFile != DefaultUsersFile {
		t.Errorf("defaults = %+v, want :8080 + manifest default", cfg)
	}
}
