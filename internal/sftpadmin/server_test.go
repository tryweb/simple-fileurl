package sftpadmin

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"simple-fileurl/internal/config"
	"simple-fileurl/internal/links"
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

func TestConfigPasswordFromSharedFile(t *testing.T) {
	dir := t.TempDir()
	shared := filepath.Join(dir, "config.json")
	sc := config.SharedConfig{
		HashAlgorithm:     "md5",
		HashTarget:        "file",
		PublicURL:         "https://example.test",
		AdminToken:        "tok",
		SftpAdminPassword: "file-pw",
	}
	if err := sc.Save(shared); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(func(k string) string {
		if k == "SHARED_CONFIG_PATH" {
			return shared
		}
		return ""
	})
	if err != nil {
		t.Fatalf("env password empty but shared file provides it: %v", err)
	}
	srv := NewServer(cfg, NewStore(filepath.Join(dir, "users.json")))
	if !srv.checkLoginPassword("file-pw") {
		t.Error("shared file password rejected")
	}
	if srv.checkLoginPassword("nope") {
		t.Error("wrong password accepted")
	}
}

func TestSeedSharedConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	env := map[string]string{
		"HASH_ALGORITHM": "sha256", "HASH_TARGET": "file",
		"PUBLIC_URL": "https://example.test", "ADMIN_TOKEN": "tok",
		"SFTP_ADMIN_PASSWORD": "pw",
	}
	getenv := func(k string) string { return env[k] }
	if err := SeedSharedConfig(path, getenv); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o640 {
		t.Fatalf("seeded config mode = %o, want 640", got)
	}
	sc, err := config.LoadShared(path)
	if err != nil {
		t.Fatal(err)
	}
	if sc.HashAlgorithm != "sha256" || sc.SftpAdminPassword != "pw" {
		t.Fatalf("seeded: %+v", sc)
	}
	// Existing files are never touched.
	sentinel := config.SharedConfig{
		HashAlgorithm: "md5", HashTarget: "filename",
		PublicURL: "https://kept.test", AdminToken: "kept",
		SftpAdminPassword: "kept",
	}
	if err := sentinel.Save(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SeedSharedConfig(path, getenv); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(path); err != nil {
		t.Fatal(err)
	} else if got := info.Mode().Perm(); got != 0o640 {
		t.Fatalf("existing config mode = %o, want 640", got)
	}
	kept, err := config.LoadShared(path)
	if err != nil {
		t.Fatal(err)
	}
	if kept != sentinel {
		t.Fatalf("existing file overwritten: %+v", kept)
	}
	// Missing secrets fail instead of seeding a broken file.
	if err := SeedSharedConfig(filepath.Join(t.TempDir(), "c.json"), func(string) string { return "" }); err == nil {
		t.Fatal("missing secrets: expected error")
	}
}

func TestCheckPasswordAgainstFailsClosedOnEmpty(t *testing.T) {
	for _, tc := range []struct {
		got, want string
		ok        bool
	}{
		{"", "", false},
		{"", "pw", false},
		{"pw", "", false},
		{"pw", "other", false},
		{"pw", "pw", true},
	} {
		if got := checkPasswordAgainst(tc.got, tc.want); got != tc.ok {
			t.Errorf("checkPasswordAgainst(%q, %q) = %v, want %v", tc.got, tc.want, got, tc.ok)
		}
	}
	if newAuthState("").checkPassword("") {
		t.Error("empty authState accepted empty candidate")
	}
	if newAuthState("").checkPassword("anything") {
		t.Error("empty authState accepted a candidate")
	}
	if newAuthState(testPassword).checkPassword("") {
		t.Error("empty candidate accepted against configured password")
	}
}

func TestLoginRejectsEmptyPassword(t *testing.T) {
	srv := newTestServer(t)
	form := url.Values{"password": {""}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), "Wrong password") {
		t.Error("empty password must re-render the prompt with an error")
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookieName && c.Value != "" && c.MaxAge >= 0 {
			t.Error("empty password must not create a session")
		}
	}
}

func TestLoginRejectsEmptyPasswordWithSharedFallback(t *testing.T) {
	dir := t.TempDir()
	shared := filepath.Join(dir, "config.json")
	sc := config.SharedConfig{
		HashAlgorithm:     "md5",
		HashTarget:        "file",
		PublicURL:         "https://example.test",
		AdminToken:        "tok",
		SftpAdminPassword: "file-pw",
	}
	if err := sc.Save(shared); err != nil {
		t.Fatal(err)
	}
	srv := NewServer(Config{Password: testPassword, SharedPath: shared}, NewStore(filepath.Join(dir, "users.json")))
	if srv.checkLoginPassword("") {
		t.Error("empty candidate accepted via shared-config fallback")
	}
	// A server misconstructed without any password still fails closed.
	bare := NewServer(Config{SharedPath: filepath.Join(dir, "missing.json")}, NewStore(filepath.Join(dir, "u2.json")))
	if bare.checkLoginPassword("") || bare.checkLoginPassword("anything") {
		t.Error("passwordless server accepted a login")
	}
}

func TestConfigRejectsRelativeSharedPath(t *testing.T) {
	_, err := LoadConfig(func(k string) string {
		switch k {
		case "SFTP_ADMIN_PASSWORD":
			return "x"
		case "SHARED_CONFIG_PATH":
			return "relative/path.json"
		}
		return ""
	})
	if err == nil {
		t.Error("relative SHARED_CONFIG_PATH accepted, want error")
	}
}

func TestLinksPageShowsShareURL(t *testing.T) {
	dir := t.TempDir()
	shared := filepath.Join(dir, "config.json")
	sc := config.SharedConfig{
		HashAlgorithm:     "md5",
		HashTarget:        "file",
		PublicURL:         "https://example.test",
		AdminToken:        "tok",
		SftpAdminPassword: testPassword,
	}
	if err := sc.Save(shared); err != nil {
		t.Fatal(err)
	}
	srv := NewServer(Config{
		Password:   testPassword,
		Addr:       ":0",
		UsersFile:  filepath.Join(dir, "users.json"),
		SharedPath: shared,
		LinksDir:   filepath.Join(dir, "links"),
	}, NewStore(filepath.Join(dir, "users.json")))
	if err := srv.links.Create(links.Link{
		ID:        "abc123",
		Scope:     links.Scope{Type: links.ScopeAdmin},
		CreatedAt: time.Now().UTC().Truncate(time.Second),
		CreatedBy: "admin",
	}); err != nil {
		t.Fatal(err)
	}
	sess, _ := loginAs(t, srv)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/links", nil)
	req.AddCookie(sess)
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /links = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "https://example.test/l/abc123") {
		t.Errorf("links page missing share URL, got:\n%s", body)
	}
	if strings.Contains(body, "no public URL") {
		t.Error("links page shows no-public-URL hint despite configured PUBLIC_URL")
	}
}

func TestLinksPageWithoutPublicURLShowsHint(t *testing.T) {
	dir := t.TempDir()
	srv := NewServer(Config{
		Password:  testPassword,
		Addr:      ":0",
		UsersFile: filepath.Join(dir, "users.json"),
		LinksDir:  filepath.Join(dir, "links"),
	}, NewStore(filepath.Join(dir, "users.json")))
	if err := srv.links.Create(links.Link{
		ID:        "abc123",
		Scope:     links.Scope{Type: links.ScopeAdmin},
		CreatedAt: time.Now().UTC().Truncate(time.Second),
		CreatedBy: "admin",
	}); err != nil {
		t.Fatal(err)
	}
	sess, _ := loginAs(t, srv)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/links", nil)
	req.AddCookie(sess)
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /links = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "no public URL") {
		t.Error("links page must show the no-public-URL hint when PUBLIC_URL is unset")
	}
}

func TestLinkCreateUsesBrowserTimezoneOffset(t *testing.T) {
	dir := t.TempDir()
	srv := NewServer(Config{
		Password:  testPassword,
		LinksDir:  filepath.Join(dir, "links"),
		UsersFile: filepath.Join(dir, "users.json"),
	}, NewStore(filepath.Join(dir, "users.json")))
	sess, csrf := loginAs(t, srv)
	rec := authedPost(srv, sess, "/links/create", url.Values{
		csrfField:        {csrf},
		"scope_type":     {"admin"},
		"expires":        {"2030-01-02T09:00"},
		"expires_offset": {"-480"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("create link = %d, want 303", rec.Code)
	}
	id := strings.TrimPrefix(rec.Header().Get("Location"), "/links?created=")
	link, ok := srv.links.Get(id)
	if !ok {
		t.Fatal("created link missing")
	}
	if link.ExpiresAt == nil {
		t.Fatal("created link has no expiry")
	}
	want := time.Date(2030, time.January, 2, 1, 0, 0, 0, time.UTC)
	if !link.ExpiresAt.Equal(want) {
		t.Errorf("expiry = %s, want %s", link.ExpiresAt.Format(time.RFC3339), want.Format(time.RFC3339))
	}
}

func TestLinksPageCompactMetadataContract(t *testing.T) {
	dir := t.TempDir()
	srv := NewServer(Config{
		Password:  testPassword,
		LinksDir:  filepath.Join(dir, "links"),
		UsersFile: filepath.Join(dir, "users.json"),
	}, NewStore(filepath.Join(dir, "users.json")))
	if err := srv.links.Create(links.Link{
		ID:           "compact",
		Scope:        links.Scope{Type: links.ScopeUser, User: "alice"},
		PasswordHash: "hash",
		CreatedAt:    time.Date(2030, time.January, 2, 1, 0, 0, 0, time.UTC),
		ExpiresAt:    func() *time.Time { v := time.Date(2030, time.January, 2, 2, 0, 0, 0, time.UTC); return &v }(),
	}); err != nil {
		t.Fatal(err)
	}
	sess, _ := loginAs(t, srv)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/links", nil)
	req.AddCookie(sess)
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /links = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`class="links-table"`,
		`.links-table{table-layout:fixed}`,
		`.link-meta-value{min-width:0;white-space:normal;overflow-wrap:anywhere;overflow:visible;text-overflow:clip}`,
		`<th scope="col">ID</th><th scope="col">URL</th><th scope="col">Access</th><th scope="col">Validity</th><th scope="col">Actions</th>`,
		`class="col-id"`,
		`class="col-url"`,
		`class="col-access"`,
		`class="col-validity"`,
		`class="col-actions"`,
		`class="link-stack"`,
		`class="link-access" data-label="Access"`,
		`class="link-validity" data-label="Validity"`,
		`class="link-actions" data-label="Actions"`,
		`@media (max-width:1024px)`,
		`.links-table colgroup{display:none}`,
		`.links-table tbody,.links-table td{display:block}`,
		`.links-table tbody tr{display:grid;grid-template-columns:minmax(0,1fr) minmax(0,2fr)}`,
		`.links-table .link-id,.links-table .link-url,.links-table .link-actions{grid-column:1 / -1}`,
		`@media (max-width:600px)`,
		`.links-table tbody tr{grid-template-columns:1fr}`,
		`Scope:`,
		`Password:`,
		`Created:`,
		`Expires:`,
		`aria-label="Scope: user, alice, sees`,
		`aria-label="Password: protected"`,
		`aria-label="Created:`,
		`aria-label="Expires:`,
		`word-break:break-all`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("links page must render %q", want)
		}
	}
	for _, unwanted := range []string{
		`.links-table{table-layout:fixed;min-width:1120px}`,
		`<th scope="col">Scope</th>`,
		`<th scope="col">Password</th>`,
		`<th scope="col">Created</th>`,
		`<th scope="col">Expires</th>`,
	} {
		if strings.Contains(body, unwanted) {
			t.Errorf("links page must not render standalone header %q", unwanted)
		}
	}
}
