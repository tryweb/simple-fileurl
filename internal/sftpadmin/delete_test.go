package sftpadmin

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// newDeleteSeeded builds a server with one enabled user (alice, two keys),
// one disabled user with keys (frozen, one key + note), and one disabled
// user without keys (empty).
func newDeleteSeeded(t *testing.T) (*Server, *http.Cookie, string) {
	t.Helper()
	srv := newTestServer(t)
	sess, csrf := loginAs(t, srv)
	seedRawManifest(t, srv, `{"version":1,"users":[`+
		`{"username":"alice","enabled":true,"authorized_keys":["`+canonOf(t, fixtureEd25519)+`","`+canonOf(t, fixtureRSA)+`"]},`+
		`{"username":"frozen","enabled":false,"authorized_keys":["`+canonOf(t, fixtureRSA)+`"]},`+
		`{"username":"empty","enabled":false,"authorized_keys":[]}]}`)
	if err := srv.store.SetNote("frozen", fixtureRSAFP, "frozen note"); err != nil {
		t.Fatal(err)
	}
	if err := srv.store.SetNote("alice", fixtureEd25519FP, "alice note"); err != nil {
		t.Fatal(err)
	}
	return srv, sess, csrf
}

func deleteUser(srv *Server, sess *http.Cookie, csrf, username string) *httptest.ResponseRecorder {
	return authedPost(srv, sess, "/users/delete", url.Values{
		csrfField:  {csrf},
		"username": {username},
	})
}

func userNames(t *testing.T, srv *Server) []string {
	t.Helper()
	m, err := srv.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, u := range m.Users {
		out = append(out, u.Username)
	}
	return out
}

func TestDeleteDisabledUserWithKeys(t *testing.T) {
	srv, sess, csrf := newDeleteSeeded(t)
	rec := deleteUser(srv, sess, csrf, "frozen")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("delete disabled = %d, want 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/" {
		t.Errorf("delete redirect = %q, want /", loc)
	}
	for _, name := range userNames(t, srv) {
		if name == "frozen" {
			t.Error("frozen entry must be gone from the manifest")
		}
	}
	if got := userNames(t, srv); len(got) != 2 || got[0] != "alice" || got[1] != "empty" {
		t.Errorf("remaining users = %v, want [alice empty]", got)
	}
	if keys, _ := mustKeys(t, srv, "alice"); len(keys) != 2 {
		t.Errorf("alice keys = %d, want untouched 2", len(keys))
	}
	notes, err := srv.store.LoadNotes()
	if err != nil {
		t.Fatal(err)
	}
	for id := range notes {
		if strings.HasPrefix(id, "frozen|") {
			t.Errorf("orphan note %q left after delete", id)
		}
	}
	if notes[keyNoteID("alice", fixtureEd25519FP)] != "alice note" {
		t.Error("alice note must survive a sibling delete")
	}
}

func TestDeleteDisabledUserNoKeys(t *testing.T) {
	srv, sess, csrf := newDeleteSeeded(t)
	if rec := deleteUser(srv, sess, csrf, "empty"); rec.Code != http.StatusSeeOther {
		t.Fatalf("delete disabled keyless = %d, want 303", rec.Code)
	}
	if got := userNames(t, srv); len(got) != 2 {
		t.Errorf("remaining users = %v, want 2", got)
	}
	for _, name := range userNames(t, srv) {
		if name == "empty" {
			t.Error("empty entry must be gone from the manifest")
		}
	}
}

func TestDeleteEnabledUserConflict(t *testing.T) {
	srv, sess, csrf := newDeleteSeeded(t)
	before := rawManifest(t, srv)
	rec := deleteUser(srv, sess, csrf, "alice")
	if rec.Code != http.StatusConflict {
		t.Errorf("delete enabled = %d, want 409", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "disable the user before deletion") {
		t.Errorf("409 body must say to disable first, got %q", rec.Body.String())
	}
	if after := rawManifest(t, srv); after != before {
		t.Error("409 delete mutated the manifest")
	}
	if keys, enabled := mustKeys(t, srv, "alice"); len(keys) != 2 || !enabled {
		t.Errorf("alice = %d keys enabled=%v, want 2 true", len(keys), enabled)
	}
}

func TestDeleteUnknownAndMalformed(t *testing.T) {
	srv, sess, csrf := newDeleteSeeded(t)
	before := rawManifest(t, srv)
	if rec := deleteUser(srv, sess, csrf, "ghost"); rec.Code != http.StatusNotFound {
		t.Errorf("unknown user = %d, want 404", rec.Code)
	}
	for _, bad := range []string{"", "Bad Name!", "root"} {
		if rec := deleteUser(srv, sess, csrf, bad); rec.Code != http.StatusBadRequest {
			t.Errorf("username %q = %d, want 400", bad, rec.Code)
		}
	}
	if after := rawManifest(t, srv); after != before {
		t.Error("failed deletes mutated the manifest")
	}
}

func TestDeleteAuthAndCSRF(t *testing.T) {
	srv, _, _ := newDeleteSeeded(t)
	anon := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/users/delete",
		strings.NewReader(url.Values{"username": {"frozen"}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.ServeHTTP(anon, req)
	if anon.Code != http.StatusUnauthorized {
		t.Errorf("anon delete = %d, want 401", anon.Code)
	}
	srv2, sess2, _ := newDeleteSeeded(t)
	before := rawManifest(t, srv2)
	if rec := deleteUser(srv2, sess2, "forged", "frozen"); rec.Code != http.StatusForbidden {
		t.Errorf("forged csrf = %d, want 403", rec.Code)
	}
	if rec := authedPost(srv2, sess2, "/users/delete", url.Values{"username": {"frozen"}}); rec.Code != http.StatusForbidden {
		t.Errorf("missing csrf = %d, want 403", rec.Code)
	}
	if after := rawManifest(t, srv2); after != before {
		t.Error("CSRF-rejected delete mutated the manifest")
	}
}

func TestDashboardDeleteOnlyOnDisabled(t *testing.T) {
	srv, sess, _ := newDeleteSeeded(t)
	body := dashboard(t, srv, sess)
	if n := strings.Count(body, `action="/users/delete"`); n != 2 {
		t.Errorf("delete forms = %d, want 2 (frozen + empty, never alice)", n)
	}
	if !strings.Contains(body, `name="username" value="frozen"`) {
		t.Error("disabled user frozen must carry a delete form")
	}
	if strings.Contains(body, `action="/users/delete" data-confirm="Delete user alice`) {
		t.Error("enabled user alice must show no Delete button")
	}
	for _, want := range []string{
		`Delete user frozen permanently? 1 key(s) will be removed. This cannot be undone.`,
		`Delete user empty permanently? 0 key(s) will be removed. This cannot be undone.`,
		`onsubmit="return confirm(this.getAttribute('data-confirm'))"`,
		`class="danger"`,
		`button.danger`,
		`>Delete<`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("dashboard must render %q", want)
		}
	}
}
