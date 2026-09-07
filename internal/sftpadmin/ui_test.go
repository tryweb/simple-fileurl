package sftpadmin

import (
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// dashboard fetches the Users page for an authenticated session.
func dashboard(t *testing.T, srv *Server, sess *http.Cookie) string {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(sess)
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("dashboard = %d, want 200", rec.Code)
	}
	// Templates escape "+" as "&#43;"; unescape so assertions can use the
	// canonical fingerprint text a browser would submit back.
	return html.UnescapeString(rec.Body.String())
}

// TestUsersPageGenerateIsPrimaryAction covers task 4.1: every existing-user
// row leads with a Generate new key action, and the generated result path
// shows the new fingerprint with a one-time download.
func TestUsersPageGenerateIsPrimaryAction(t *testing.T) {
	srv := newTestServer(t)
	srv.gen = stubGen(fixtureEd25519)
	sess, csrf := loginAs(t, srv)
	seedRawManifest(t, srv, `{"version":1,"users":[{"username":"alice","enabled":true,"authorized_keys":["`+canonOf(t, fixtureRSA)+`"]}]}`)

	body := dashboard(t, srv, sess)
	if !strings.Contains(body, `action="/users/generate-key"`) {
		t.Fatal("users page has no generate-key form")
	}
	if !strings.Contains(body, "Generate new key") {
		t.Error("generate action must be labelled Generate new key")
	}
	if !strings.Contains(body, `name="username" value="alice"`) {
		t.Error("generate form must carry the row username")
	}
	if strings.Index(body, "Generate new key") > strings.Index(body, "Advanced: add external key") {
		t.Error("generate must be the primary row action, ahead of the advanced flow")
	}

	rec := authedPost(srv, sess, "/users/generate-key", url.Values{
		csrfField:  {csrf},
		"username": {"alice"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("generate = %d, want 200 with one-time download page", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), fixtureEd25519FP) {
		t.Error("generate result must show the new fingerprint")
	}
	if m := tokenRe.FindStringSubmatch(rec.Body.String()); m == nil {
		t.Error("generate result must link the one-time download")
	}

	srv.gen = stubGen(fixtureECDSA)
	seedRawManifest(t, srv, `{"version":1,"users":[{"username":"frozen","enabled":false,"authorized_keys":[]}]}`)
	if rec := authedPost(srv, sess, "/users/generate-key", url.Values{
		csrfField:  {csrf},
		"username": {"frozen"},
	}); rec.Code != http.StatusOK {
		t.Errorf("generate for disabled = %d, want 200", rec.Code)
	}
	if _, enabled := mustKeys(t, srv, "frozen"); enabled {
		t.Error("generating a key must not re-enable a disabled user")
	}
}

// TestUsersPageAdvancedCompatibility covers task 4.1: the create-user
// paste-or-generate flow is unchanged and the existing-user external-key
// form remains as an explicitly advanced migration path.
func TestUsersPageAdvancedCompatibility(t *testing.T) {
	srv := newTestServer(t)
	sess, csrf := loginAs(t, srv)
	seedRawManifest(t, srv, `{"version":1,"users":[{"username":"alice","enabled":true,"authorized_keys":["`+canonOf(t, fixtureRSA)+`"]}]}`)

	body := dashboard(t, srv, sess)
	if !strings.Contains(body, `action="/users/create"`) {
		t.Fatal("users page must keep the create-user form")
	}
	if !strings.Contains(body, `name="public_key"`) {
		t.Error("create-user form must keep the public_key field")
	}
	if !strings.Contains(body, "Advanced") {
		t.Error("paste flows must be marked Advanced compatibility")
	}
	if !strings.Contains(body, `action="/users/add-key"`) {
		t.Fatal("users page must keep the advanced add-key form")
	}

	if rec := authedPost(srv, sess, "/users/create", url.Values{
		csrfField:    {csrf},
		"username":   {"bob"},
		"public_key": {fixtureEd25519},
	}); rec.Code != http.StatusSeeOther {
		t.Errorf("create with pasted key = %d, want 303", rec.Code)
	}
	if rec := authedPost(srv, sess, "/users/add-key", url.Values{
		csrfField:    {csrf},
		"username":   {"bob"},
		"public_key": {fixtureRSA},
	}); rec.Code != http.StatusSeeOther {
		t.Errorf("advanced add-key = %d, want 303", rec.Code)
	}
	if keys, _ := mustKeys(t, srv, "bob"); len(keys) != 2 {
		t.Errorf("bob keys = %d, want pasted plus advanced", len(keys))
	}
}

// TestUsersPageValidKeyRemoveForms covers task 4.2: every valid key renders
// its canonical fingerprint and key type with an individual Remove action
// carrying username, fingerprint, and CSRF.
func TestUsersPageValidKeyRemoveForms(t *testing.T) {
	srv := newTestServer(t)
	sess, _ := loginAs(t, srv)
	seedRawManifest(t, srv, `{"version":1,"users":[{"username":"alice","enabled":true,"authorized_keys":["`+canonOf(t, fixtureEd25519)+`","`+canonOf(t, fixtureRSA)+`"]}]}`)

	body := dashboard(t, srv, sess)
	for _, want := range []string{fixtureEd25519FP, fixtureRSAFP, "ssh-ed25519", "ssh-rsa"} {
		if !strings.Contains(body, want) {
			t.Errorf("users page must render %q", want)
		}
	}
	if n := strings.Count(body, `action="/users/delete-key"`); n != 2 {
		t.Errorf("delete-key forms = %d, want one per valid key", n)
	}
	for _, fp := range []string{fixtureEd25519FP, fixtureRSAFP} {
		if !strings.Contains(body, `name="fingerprint" value="`+fp+`"`) {
			t.Errorf("users page must carry fingerprint %s in a remove form", fp)
		}
	}
	if !strings.Contains(body, `name="`+csrfField+`"`) {
		t.Error("remove forms must carry CSRF tokens")
	}
}

// TestUsersPageInvalidRepairAction covers task 4.3: invalid entries render
// an explicit invalid state with no per-key action, and one manifest-wide
// repair action explains its scope.
func TestUsersPageInvalidRepairAction(t *testing.T) {
	srv := newTestServer(t)
	sess, csrf := loginAs(t, srv)
	seedRawManifest(t, srv, `{"version":1,"users":[`+
		`{"username":"legacy","enabled":true,"authorized_keys":["nope"]},`+
		`{"username":"alice","enabled":true,"authorized_keys":["`+canonOf(t, fixtureEd25519)+`"]}]}`)

	body := dashboard(t, srv, sess)
	for _, want := range []string{"Invalid key", "(invalid)", "no fingerprint", fixtureEd25519FP} {
		if !strings.Contains(body, want) {
			t.Errorf("users page must render %q", want)
		}
	}
	if !strings.Contains(body, `action="/users/remove-invalid-keys"`) {
		t.Fatal("users page must expose the manifest-wide repair action")
	}
	if !strings.Contains(body, "manifest") {
		t.Error("repair action must state its manifest-wide scope")
	}
	if n := strings.Count(body, `action="/users/delete-key"`); n != 1 {
		t.Errorf("delete-key forms = %d, want only the one valid key (no per-invalid-key action)", n)
	}

	if rec := authedPost(srv, sess, "/users/remove-invalid-keys", url.Values{csrfField: {csrf}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("repair = %d, want 303", rec.Code)
	}
	after := dashboard(t, srv, sess)
	if strings.Contains(after, "Invalid key") {
		t.Error("repair must clear the invalid state")
	}
	if !strings.Contains(after, fixtureEd25519FP) {
		t.Error("repair must preserve the valid key")
	}
	if keys, enabled := mustKeys(t, srv, "legacy"); len(keys) != 0 || enabled {
		t.Errorf("all-invalid user = %v enabled=%v, want empty and disabled", keys, enabled)
	}
}

// TestUsersPageDisableEnableControls covers task 4.2: Disable and Enable
// stay separate, explicit controls with working status transitions.
func TestUsersPageDisableEnableControls(t *testing.T) {
	srv := newTestServer(t)
	sess, csrf := loginAs(t, srv)
	seedRawManifest(t, srv, `{"version":1,"users":[`+
		`{"username":"alice","enabled":true,"authorized_keys":["`+canonOf(t, fixtureEd25519)+`"]},`+
		`{"username":"frozen","enabled":false,"authorized_keys":["`+canonOf(t, fixtureRSA)+`"]}]}`)

	body := dashboard(t, srv, sess)
	if !strings.Contains(body, `name="action" value="disable"`) {
		t.Error("enabled user must show a Disable control")
	}
	if !strings.Contains(body, `name="action" value="enable"`) {
		t.Error("disabled user must show an Enable control")
	}
	if !strings.Contains(body, ">Disable<") || !strings.Contains(body, ">Enable<") {
		t.Error("Disable/Enable labels must stay distinct")
	}

	flip := func(user, action string) int {
		return authedPost(srv, sess, "/users/status", url.Values{
			csrfField:  {csrf},
			"username": {user},
			"action":   {action},
		}).Code
	}
	if code := flip("alice", "disable"); code != http.StatusSeeOther {
		t.Errorf("disable = %d, want 303", code)
	}
	if code := flip("frozen", "enable"); code != http.StatusSeeOther {
		t.Errorf("enable = %d, want 303", code)
	}
	after := dashboard(t, srv, sess)
	if strings.Count(after, `name="action" value="disable"`) != 1 || strings.Count(after, `name="action" value="enable"`) != 1 {
		t.Error("status transitions must swap the Disable/Enable controls")
	}
}
