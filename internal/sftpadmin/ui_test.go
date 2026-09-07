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
// row leads with a Generate new key action, and generating for an existing
// user stays on the Users dashboard (HTTP 200) with a top-of-page result
// card carrying the new fingerprint and the one-time download link.
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
	if strings.Contains(body, `action="/users/add-key"`) {
		t.Error("row-level add-key forms are removed; external keys enter via create-user paste or the API endpoint")
	}
	if strings.Contains(body, "Key generated for") {
		t.Error("plain dashboard must render no generated-result card")
	}
	if m := tokenRe.FindStringSubmatch(body); m != nil {
		t.Error("plain dashboard must carry no download token")
	}

	rec := authedPost(srv, sess, "/users/generate-key", url.Values{
		csrfField:  {csrf},
		"username": {"alice"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("generate = %d, want 200 rendering the dashboard with the result card", rec.Code)
	}
	got := html.UnescapeString(rec.Body.String())
	if !strings.Contains(got, fixtureEd25519FP) {
		t.Error("generate result must show the new fingerprint")
	}
	if m := tokenRe.FindStringSubmatch(got); m == nil {
		t.Error("generate result must link the one-time download")
	}
	for _, want := range []string{
		"Key generated for alice",
		"Download private key (one-time)",
		">Close<",
		"The private key below can be downloaded exactly once. It is never stored. Save it now.",
		"<table",
		`action="/users/generate-key"`,
		fixtureRSAFP,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("same-page generate result must render %q", want)
		}
	}
	if !strings.Contains(got, `<a class="btn secondary" href="/">Close</a>`) {
		t.Error("generate result card must dismiss via a Close secondary-button anchor to /")
	}
	if strings.Contains(got, "Back to users") {
		t.Error("generate result card must not keep the old Back to users link")
	}
	if loc := rec.Header().Get("Location"); loc != "" {
		t.Errorf("generate must render via 200 body, got redirect to %q", loc)
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

	for _, tc := range []struct {
		name     string
		username string
		code     int
	}{
		{"unknown user", "ghost", http.StatusNotFound},
		{"malformed username", "Bad Name!", http.StatusBadRequest},
	} {
		failed := authedPost(srv, sess, "/users/generate-key", url.Values{
			csrfField:  {csrf},
			"username": {tc.username},
		})
		if failed.Code != tc.code {
			t.Errorf("generate for %s = %d, want %d", tc.name, failed.Code, tc.code)
		}
		failedBody := failed.Body.String()
		if strings.Contains(failedBody, "Key generated for") {
			t.Errorf("failed generate for %s must render no result card", tc.name)
		}
		if m := tokenRe.FindStringSubmatch(failedBody); m != nil {
			t.Errorf("failed generate for %s must carry no download token", tc.name)
		}
	}
}

// TestUsersPageAdvancedCompatibility covers the paste-or-generate contract:
// the create-user paste field stays, the row-level external-key forms are
// gone from the page, and POST /users/add-key still works as an
// API-compatible endpoint.
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
	if !strings.Contains(body, "paste") {
		t.Error("create-user form must keep its paste guidance")
	}
	if strings.Contains(body, `action="/users/add-key"`) {
		t.Error("users page must not render row-level add-key forms")
	}
	if strings.Contains(body, "Advanced: add external key") {
		t.Error("users page must not render the removed advanced add-key disclosure")
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
		t.Errorf("add-key endpoint = %d, want 303 (API compat retained)", rec.Code)
	}
	if keys, _ := mustKeys(t, srv, "bob"); len(keys) != 2 {
		t.Errorf("bob keys = %d, want pasted plus endpoint-added", len(keys))
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

func TestUsersPageLayoutContract(t *testing.T) {
	srv := newTestServer(t)
	sess, _ := loginAs(t, srv)
	seedRawManifest(t, srv, `{"version":1,"users":[{"username":"alice","enabled":true,"authorized_keys":["`+canonOf(t, fixtureEd25519)+`"]}]}`)

	body := dashboard(t, srv, sess)
	for _, want := range []string{
		`main{max-width:1200px`,
		`.table-wrap{overflow-x:auto}`,
		`<table class="users-table">`,
		`<colgroup>`,
		`<col class="col-username">`,
		`<col class="col-status">`,
		`<col class="col-keys">`,
		`<col class="col-manage">`,
		`.users-table{table-layout:fixed}`,
		`.users-table .col-username{width:24%}`,
		`.users-table .col-status{width:18%}`,
		`.users-table .col-keys{width:22%}`,
		`.users-table .col-manage{width:36%}`,
		`<th scope="col">Manage</th>`,
		`<td colspan="4" class="user-cell">`,
		`<summary class="user-summary">`,
		`.user-cell{padding:0}`,
		`.user-summary{display:grid`,
		`grid-template-columns:24% 18% 22% 36%`,
		`grid-template-columns:repeat(2,minmax(0,1fr))`,
		`.user-summary .summary-manage::before{content:"▸"`,
		`.user-details[open] .summary-manage::before{content:"▾"}`,
		`list-style:none`,
		`.users-table thead{position:absolute`,
		`.user-maintenance .inline-form input[type=text]{width:100%;max-width:100%}`,
		`.user-details[open] .user-maintenance{border-top:1px solid var(--line)`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("users page must render %q (wide shell + class-based column allocation)", want)
		}
	}
	for _, gone := range []string{
		`<td><details class="user-details">`,
		`<col style="width:12%">`,
		`<col style="width:10%">`,
		`<col style="width:46%">`,
		`<col style="width:32%">`,
		`<table style="table-layout:fixed">`,
		`<th scope="col">Actions</th>`,
	} {
		if strings.Contains(body, gone) {
			t.Errorf("users page must not render %q (inline styles and Actions header are removed)", gone)
		}
	}
	if strings.Contains(body, "max-width:960px") {
		t.Error("users page must not keep the old 960px shell")
	}
	if strings.Contains(body, "Key generated for") {
		t.Error("plain dashboard must render no generated-result card")
	}
	if strings.Contains(body, ">Close<") {
		t.Error("plain dashboard carries no card, so it must carry no Close control either")
	}
	if m := tokenRe.FindStringSubmatch(body); m != nil {
		t.Error("plain dashboard must carry no download token")
	}
}

// TestUsersPageScanFirstStructure locks the scan-first workspace order:
// Existing users is the first and dominant card, each row scans as
// username + status + key summary, and every maintenance control sits
// behind a native Manage disclosure. Reverting the card order, dropping
// the hooks, flattening the summaries, or inlining maintenance must fail.
func TestUsersPageScanFirstStructure(t *testing.T) {
	srv := newTestServer(t)
	sess, _ := loginAs(t, srv)
	seedRawManifest(t, srv, `{"version":1,"users":[`+
		`{"username":"alice","enabled":true,"authorized_keys":["`+canonOf(t, fixtureEd25519)+`"]},`+
		`{"username":"bob","enabled":true,"authorized_keys":["`+canonOf(t, fixtureEd25519)+`","`+canonOf(t, fixtureRSA)+`"]},`+
		`{"username":"empty","enabled":true,"authorized_keys":[]},`+
		`{"username":"legacy","enabled":false,"authorized_keys":["nope"]}]}`)

	body := dashboard(t, srv, sess)

	for _, want := range []string{
		`id="existing-users"`,
		`id="create-user"`,
		`id="key-repair"`,
		`class="users-table"`,
		`class="key-summary"`,
		`<td colspan="4" class="user-cell"><details class="user-details">`,
		`<summary class="user-summary">`,
		`<span class="summary-manage">Manage alice</span>`,
		`<span class="summary-manage">Manage bob</span>`,
		`<span class="summary-manage">Manage empty</span>`,
		`<span class="summary-manage">Manage legacy</span>`,
		`<div class="user-maintenance">`,
		`class="user-actions"`,
		`grid-template-columns:repeat(auto-fit,minmax(280px,1fr))`,
		`1 usable key`,
		`2 usable keys`,
		`No keys`,
		`1 invalid`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("scan-first users page must render %q", want)
		}
	}

	existing := strings.Index(body, `id="existing-users"`)
	create := strings.Index(body, `id="create-user"`)
	repair := strings.Index(body, `id="key-repair"`)
	if existing < 0 || create < 0 || repair < 0 {
		t.Fatal("scan-first users page must carry all three card hooks")
	}
	if !(existing < create && create < repair) {
		t.Error("card order must be Existing users, then Create user, then Key repair")
	}

	for _, user := range []string{"alice", "bob", "empty", "legacy"} {
		summary := strings.Index(body, "Manage "+user+"</span>")
		if summary < 0 {
			t.Errorf("row for %s must carry a Manage disclosure", user)
			continue
		}
		for _, form := range []string{`action="/users/generate-key"`, `action="/users/status"`} {
			after := strings.Index(body[summary:], form)
			// The outer disclosure closes with </div></details>; inner
			// per-key "Show full key" details close with a bare
			// </details> and must not end the search.
			next := strings.Index(body[summary:], "</div></details>")
			if after < 0 || (next >= 0 && after > next) {
				t.Errorf("row for %s must keep %s inside its Manage disclosure", user, form)
			}
		}
	}
	if n := strings.Count(body, `<details class="user-details">`); n != 4 {
		t.Errorf("user disclosures = %d, want one per user", n)
	}
	if n := strings.Count(body, `<td colspan="4" class="user-cell">`); n != 4 {
		t.Errorf("spanning cells = %d, want one full-width colspan cell per user", n)
	}
	if strings.Contains(body, `<td><details class="user-details">`) {
		t.Error("Manage disclosure must not be constrained inside a single Manage column cell")
	}
	if !strings.Contains(body, `<thead><tr><th scope="col">Username</th>`) {
		t.Error("spanning rows must retain the table header")
	}
	// The compact summary carries the counts; the full fingerprints stay
	// one disclosure level down with their forms.
	if !strings.Contains(body, fixtureEd25519FP) {
		t.Error("disclosed maintenance must still render the full fingerprint")
	}
}
