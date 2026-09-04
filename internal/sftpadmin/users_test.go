package sftpadmin

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"
)

var tokenRe = regexp.MustCompile(`token=([A-Za-z0-9_-]+)`)

func createUser(t *testing.T, srv *Server, sess *http.Cookie, csrf, username, pubkey string) *httptest.ResponseRecorder {
	t.Helper()
	form := url.Values{csrfField: {csrf}, "username": {username}}
	if pubkey != "" {
		form.Set("public_key", pubkey)
	}
	return authedPost(srv, sess, "/users/create", form)
}

func TestCreateGenerateKeyOneTime(t *testing.T) {
	srv := newTestServer(t)
	sess, csrf := loginAs(t, srv)

	rec := createUser(t, srv, sess, csrf, "alice", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("create = %d, want 200 with one-time link", rec.Code)
	}
	m := tokenRe.FindStringSubmatch(rec.Body.String())
	if m == nil {
		t.Fatal("created page has no download token")
	}
	token := m[1]

	get := func() *httptest.ResponseRecorder {
		r := httptest.NewRecorder()
		q := httptest.NewRequest(http.MethodGet, "/keys/download?token="+token, nil)
		q.AddCookie(sess)
		srv.ServeHTTP(r, q)
		return r
	}
	first := get()
	if first.Code != http.StatusOK {
		t.Fatalf("first download = %d, want 200", first.Code)
	}
	if cd := first.Header().Get("Content-Disposition"); !strings.Contains(cd, "attachment") {
		t.Errorf("download missing attachment disposition: %q", cd)
	}
	private := first.Body.String()
	if !strings.Contains(private, "PRIVATE KEY") {
		t.Error("downloaded body is not a private key")
	}
	if second := get(); second.Code != http.StatusNotFound {
		t.Errorf("second download = %d, want 404", second.Code)
	}

	// The manifest keeps only the public half; the private half appears
	// nowhere on disk.
	raw, err := os.ReadFile(srv.store.Path())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "PRIVATE KEY") || strings.Contains(string(raw), private) {
		t.Error("private key material leaked into the manifest file")
	}
	st, err := srv.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Users) != 1 || len(st.Users[0].AuthorizedKeys) != 1 {
		t.Fatalf("manifest = %+v, want one user with one key", st)
	}
	if _, _, err := ValidatePublicKey(st.Users[0].AuthorizedKeys[0]); err != nil {
		t.Errorf("stored key invalid: %v", err)
	}
}

func TestCreateValidation(t *testing.T) {
	srv := newTestServer(t)
	sess, csrf := loginAs(t, srv)
	kp, err := GenerateEd25519KeyPair("t")
	if err != nil {
		t.Fatal(err)
	}
	for name, tc := range map[string]struct {
		username, pubkey string
		want             int
	}{
		"reserved":  {"root", kp.PublicKey, http.StatusBadRequest},
		"uppercase": {"Alice", kp.PublicKey, http.StatusBadRequest},
		"bad char":  {"a b", kp.PublicKey, http.StatusBadRequest},
		"bad key":   {"dave", "not-a-key", http.StatusBadRequest},
		"private":   {"erin", kp.PrivatePEM, http.StatusBadRequest},
		"ok pasted": {"bob", kp.PublicKey, http.StatusSeeOther},
	} {
		if rec := createUser(t, srv, sess, csrf, tc.username, tc.pubkey); rec.Code != tc.want {
			t.Errorf("%s: create = %d, want %d", name, rec.Code, tc.want)
		}
	}
	m, err := srv.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Users) != 1 || m.Users[0].Username != "bob" {
		t.Errorf("manifest = %+v, want only bob", m)
	}
	if rec := createUser(t, srv, sess, csrf, "bob", kp.PublicKey); rec.Code != http.StatusConflict {
		t.Errorf("duplicate create = %d, want 409", rec.Code)
	}
}

func TestUnauthenticatedMutationRejected(t *testing.T) {
	srv := newTestServer(t)
	form := url.Values{"username": {"mallory"}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/users/create", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("anon create = %d, want 401", rec.Code)
	}
}

func TestAddKeyDedupAndMultiple(t *testing.T) {
	srv := newTestServer(t)
	sess, csrf := loginAs(t, srv)
	ka, err := GenerateEd25519KeyPair("alice")
	if err != nil {
		t.Fatal(err)
	}
	kb, err := GenerateEd25519KeyPair("alice-second")
	if err != nil {
		t.Fatal(err)
	}
	if rec := createUser(t, srv, sess, csrf, "alice", ka.PublicKey); rec.Code != http.StatusSeeOther {
		t.Fatalf("create = %d", rec.Code)
	}
	add := func(key string) int {
		return authedPost(srv, sess, "/users/add-key", url.Values{
			csrfField:    {csrf},
			"username":   {"alice"},
			"public_key": {key},
		}).Code
	}
	if code := add(ka.PublicKey); code != http.StatusSeeOther {
		t.Fatalf("re-add same key = %d", code)
	}
	count := func() int {
		m, err := srv.store.Load()
		if err != nil {
			t.Fatal(err)
		}
		return len(m.Users[0].AuthorizedKeys)
	}
	if n := count(); n != 1 {
		t.Fatalf("after dedup keys = %d, want 1", n)
	}
	if code := add(kb.PublicKey); code != http.StatusSeeOther {
		t.Fatalf("add second key = %d", code)
	}
	if n := count(); n != 2 {
		t.Fatalf("after second key keys = %d, want 2", n)
	}
	if code := add("garbage"); code != http.StatusBadRequest {
		t.Errorf("malformed key = %d, want 400", code)
	}
	if code := add(kb.PrivatePEM); code != http.StatusBadRequest {
		t.Errorf("private key = %d, want 400", code)
	}
	if n := count(); n != 2 {
		t.Errorf("after rejected keys = %d, want still 2", n)
	}
	if code := authedPost(srv, sess, "/users/add-key", url.Values{
		csrfField:    {csrf},
		"username":   {"ghost"},
		"public_key": {kb.PublicKey},
	}).Code; code != http.StatusNotFound {
		t.Errorf("unknown user = %d, want 404", code)
	}
}

func TestDisableEnable(t *testing.T) {
	srv := newTestServer(t)
	sess, csrf := loginAs(t, srv)
	kp, err := GenerateEd25519KeyPair("alice")
	if err != nil {
		t.Fatal(err)
	}
	if rec := createUser(t, srv, sess, csrf, "alice", kp.PublicKey); rec.Code != http.StatusSeeOther {
		t.Fatalf("create = %d", rec.Code)
	}
	flip := func(action string) int {
		return authedPost(srv, sess, "/users/status", url.Values{
			csrfField:  {csrf},
			"username": {"alice"},
			"action":   {action},
		}).Code
	}
	enabled := func() bool {
		m, err := srv.store.Load()
		if err != nil {
			t.Fatal(err)
		}
		return m.Users[0].Enabled
	}
	if code := flip("disable"); code != http.StatusSeeOther || enabled() {
		t.Errorf("disable = %d enabled=%v, want 303 false", code, enabled())
	}
	if code := flip("enable"); code != http.StatusSeeOther || !enabled() {
		t.Errorf("enable = %d enabled=%v, want 303 true", code, enabled())
	}
	if code := flip("explode"); code != http.StatusBadRequest {
		t.Errorf("bad action = %d, want 400", code)
	}
	if code := authedPost(srv, sess, "/users/status", url.Values{
		csrfField:  {csrf},
		"username": {"ghost"},
		"action":   {"disable"},
	}).Code; code != http.StatusNotFound {
		t.Errorf("unknown user = %d, want 404", code)
	}
}
