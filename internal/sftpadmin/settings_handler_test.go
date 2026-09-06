package sftpadmin

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"simple-fileurl/internal/config"
)

func newSettingsServer(t *testing.T) (*Server, string) {
	t.Helper()
	srv := newTestServer(t)
	shared := filepath.Join(t.TempDir(), "config.json")
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
	srv.sharedPath = shared
	return srv, shared
}

func TestSettingsRequiresAuth(t *testing.T) {
	srv, _ := newSettingsServer(t)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/settings", nil))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("unauth settings: %d", rec.Code)
	}
}

func TestSettingsPageShowsValues(t *testing.T) {
	srv, _ := newSettingsServer(t)
	cookie, _ := loginAs(t, srv)
	req := httptest.NewRequest(http.MethodGet, "/settings", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("settings page: %d", rec.Code)
	}
	body := rec.Body.String()
	for _, key := range []string{"HASH_ALGORITHM", "HASH_TARGET", "PUBLIC_URL", "ADMIN_TOKEN", "SFTP_ADMIN_PASSWORD", `href="/settings"`} {
		if !strings.Contains(body, key) {
			t.Fatalf("settings page missing %q", key)
		}
	}
	if !strings.Contains(body, "https://example.test") {
		t.Fatal("current PUBLIC_URL not shown")
	}
	if strings.Contains(body, `"tok"`) || strings.Contains(body, testPassword) {
		t.Fatal("secret value leaked into page")
	}
}

func TestSettingsSaveValid(t *testing.T) {
	srv, shared := newSettingsServer(t)
	cookie, csrf := loginAs(t, srv)
	form := url.Values{csrfField: {csrf}, "PUBLIC_URL": {"https://new.test"}, "HASH_ALGORITHM": {"sha256"}}
	req := httptest.NewRequest(http.MethodPost, "/settings", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("save: %d %q", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Saved:") {
		t.Fatalf("no saved notice: %q", rec.Body.String())
	}
	sc, err := config.LoadShared(shared)
	if err != nil {
		t.Fatal(err)
	}
	if sc.PublicURL != "https://new.test" || sc.HashAlgorithm != "sha256" {
		t.Fatalf("not persisted: %+v", sc)
	}
}

func TestSettingsSaveInvalid(t *testing.T) {
	srv, shared := newSettingsServer(t)
	cookie, csrf := loginAs(t, srv)
	form := url.Values{csrfField: {csrf}, "HASH_ALGORITHM": {"sha1"}}
	req := httptest.NewRequest(http.MethodPost, "/settings", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("save: %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "HASH_ALGORITHM") {
		t.Fatalf("field error not shown: %q", rec.Body.String())
	}
	sc, err := config.LoadShared(shared)
	if err != nil {
		t.Fatal(err)
	}
	if sc.HashAlgorithm != "md5" {
		t.Fatalf("rejected value persisted: %+v", sc)
	}
}

func TestSettingsSaveSecretUnchangedWhenEmpty(t *testing.T) {
	srv, shared := newSettingsServer(t)
	cookie, csrf := loginAs(t, srv)
	form := url.Values{csrfField: {csrf}, "ADMIN_TOKEN": {""}, "PUBLIC_URL": {"https://example.test"}}
	req := httptest.NewRequest(http.MethodPost, "/settings", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("save: %d", rec.Code)
	}
	sc, err := config.LoadShared(shared)
	if err != nil {
		t.Fatal(err)
	}
	if sc.AdminToken != "tok" {
		t.Fatalf("empty secret overwrote value: %+v", sc)
	}
}

func TestSettingsSaveRequiresCSRF(t *testing.T) {
	srv, _ := newSettingsServer(t)
	cookie, _ := loginAs(t, srv)
	form := url.Values{"PUBLIC_URL": {"https://evil.test"}}
	req := httptest.NewRequest(http.MethodPost, "/settings", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("missing csrf: %d", rec.Code)
	}
}

func TestSettingsSaveLogsNoRawTokens(t *testing.T) {
	srv, _ := newSettingsServer(t)
	cookie, csrf := loginAs(t, srv)
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)
	form := url.Values{csrfField: {csrf}, "PUBLIC_URL": {"https://new.test"}}
	req := httptest.NewRequest(http.MethodPost, "/settings", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("save: %d", rec.Code)
	}
	if out := buf.String(); strings.Contains(out, csrf) {
		t.Errorf("settings save logged the raw CSRF token: %q", out)
	}
}
