package sftpadmin

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// rawDashboard fetches the Users page without unescaping entities, so
// escaping assertions see exactly what the server sent.
func rawDashboard(t *testing.T, srv *Server, sess *http.Cookie) string {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(sess)
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("dashboard = %d, want 200", rec.Code)
	}
	return rec.Body.String()
}

// saveNote posts one operator note and returns the response.
func saveNote(t *testing.T, srv *Server, sess *http.Cookie, csrf, username, fp, note string) *httptest.ResponseRecorder {
	t.Helper()
	return authedPost(srv, sess, "/users/key-note", url.Values{
		csrfField:     {csrf},
		"username":    {username},
		"fingerprint": {fp},
		"note":        {note},
	})
}

func seedAlice(t *testing.T, srv *Server) {
	t.Helper()
	seedRawManifest(t, srv, `{"version":1,"users":[{"username":"alice","enabled":true,"authorized_keys":["`+canonOf(t, fixtureEd25519)+`"]}]}`)
}

// TestKeyNoteSaveRenderRoundTrip covers the notes primitive: a saved note
// renders under its key, is HTML-escaped, and persists in the sidecar file
// next to the manifest (never inside it).
func TestKeyNoteSaveRenderRoundTrip(t *testing.T) {
	srv := newTestServer(t)
	sess, csrf := loginAs(t, srv)
	seedAlice(t, srv)

	if rec := saveNote(t, srv, sess, csrf, "alice", fixtureEd25519FP, "laptop <b>ed</b>"); rec.Code != http.StatusSeeOther {
		t.Fatalf("save note = %d, want 303", rec.Code)
	}
	body := dashboard(t, srv, sess)
	if !strings.Contains(body, "Note: laptop <b>ed</b>") {
		t.Error("dashboard must render the note under its key")
	}
	raw := rawDashboard(t, srv, sess)
	if !strings.Contains(raw, "Note: laptop &lt;b&gt;ed&lt;/b&gt;") {
		t.Error("dashboard must escape note HTML")
	}
	if strings.Contains(raw, "Note: laptop <b>") {
		t.Error("dashboard rendered raw HTML from the note")
	}
	if !strings.Contains(body, `action="/users/key-note"`) {
		t.Error("valid key row must carry the save-note form")
	}

	notes, err := srv.store.LoadNotes()
	if err != nil {
		t.Fatal(err)
	}
	if got := notes[keyNoteID("alice", fixtureEd25519FP)]; got != "laptop <b>ed</b>" {
		t.Errorf("sidecar note = %q, want raw stored value", got)
	}
	sidecar := filepath.Join(filepath.Dir(srv.store.Path()), "key-notes.json")
	if _, err := os.Stat(sidecar); err != nil {
		t.Errorf("sidecar file missing at %s: %v", sidecar, err)
	}
	if raw := rawManifest(t, srv); strings.Contains(raw, "laptop") {
		t.Error("note leaked into the manifest file")
	}
}

// TestKeyNoteLengthBoundary covers the 120-character limit in runes:
// exactly 120 saves, 121 is a 400 that stores nothing.
func TestKeyNoteLengthBoundary(t *testing.T) {
	srv := newTestServer(t)
	sess, csrf := loginAs(t, srv)
	seedAlice(t, srv)

	if rec := saveNote(t, srv, sess, csrf, "alice", fixtureEd25519FP, strings.Repeat("é", 120)); rec.Code != http.StatusSeeOther {
		t.Fatalf("120-rune note = %d, want 303", rec.Code)
	}
	if rec := saveNote(t, srv, sess, csrf, "alice", fixtureEd25519FP, strings.Repeat("é", 121)); rec.Code != http.StatusBadRequest {
		t.Fatalf("121-rune note = %d, want 400", rec.Code)
	}
	notes, err := srv.store.LoadNotes()
	if err != nil {
		t.Fatal(err)
	}
	if got := notes[keyNoteID("alice", fixtureEd25519FP)]; len([]rune(got)) != 120 {
		t.Errorf("overlong note overwrote the saved note: %q", got)
	}
}

// TestKeyNoteUnknownTargets covers 404s: unknown users and fingerprints
// that match no valid key store nothing.
func TestKeyNoteUnknownTargets(t *testing.T) {
	srv := newTestServer(t)
	sess, csrf := loginAs(t, srv)
	seedAlice(t, srv)

	if rec := saveNote(t, srv, sess, csrf, "ghost", fixtureEd25519FP, "x"); rec.Code != http.StatusNotFound {
		t.Errorf("unknown user = %d, want 404", rec.Code)
	}
	if rec := saveNote(t, srv, sess, csrf, "alice", fixtureRSAFP, "x"); rec.Code != http.StatusNotFound {
		t.Errorf("unknown key = %d, want 404", rec.Code)
	}
	if rec := saveNote(t, srv, sess, csrf, "alice", "nope", "x"); rec.Code != http.StatusBadRequest {
		t.Errorf("malformed fingerprint = %d, want 400", rec.Code)
	}
	if notes, err := srv.store.LoadNotes(); err != nil {
		t.Fatal(err)
	} else if len(notes) != 0 {
		t.Errorf("failed saves stored notes: %+v", notes)
	}
}

// TestKeyNoteAuth covers the endpoint contract: anonymous callers get 401,
// forged CSRF gets 403 without mutation.
func TestKeyNoteAuth(t *testing.T) {
	srv := newTestServer(t)
	sess, _ := loginAs(t, srv)
	seedAlice(t, srv)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/users/key-note",
		strings.NewReader(url.Values{"username": {"alice"}, "fingerprint": {fixtureEd25519FP}, "note": {"x"}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("anon note = %d, want 401", rec.Code)
	}
	if rec := saveNote(t, srv, sess, "forged", "alice", fixtureEd25519FP, "x"); rec.Code != http.StatusForbidden {
		t.Errorf("forged csrf = %d, want 403", rec.Code)
	}
	if notes, err := srv.store.LoadNotes(); err != nil {
		t.Fatal(err)
	} else if len(notes) != 0 {
		t.Error("rejected note mutated the sidecar")
	}
}

// TestKeyNoteClearAndDeleteCleanup covers note lifecycle: an empty note
// clears the entry, and removing the key deletes its note.
func TestKeyNoteClearAndDeleteCleanup(t *testing.T) {
	srv := newTestServer(t)
	sess, csrf := loginAs(t, srv)
	seedAlice(t, srv)

	if rec := saveNote(t, srv, sess, csrf, "alice", fixtureEd25519FP, "temp"); rec.Code != http.StatusSeeOther {
		t.Fatalf("save = %d", rec.Code)
	}
	if rec := saveNote(t, srv, sess, csrf, "alice", fixtureEd25519FP, ""); rec.Code != http.StatusSeeOther {
		t.Fatalf("clear = %d", rec.Code)
	}
	if body := dashboard(t, srv, sess); strings.Contains(body, "Note: temp") {
		t.Error("cleared note still renders")
	}

	if rec := saveNote(t, srv, sess, csrf, "alice", fixtureEd25519FP, "again"); rec.Code != http.StatusSeeOther {
		t.Fatalf("re-save = %d", rec.Code)
	}
	if rec := authedPost(srv, sess, "/users/delete-key", url.Values{
		csrfField:     {csrf},
		"username":    {"alice"},
		"fingerprint": {fixtureEd25519FP},
	}); rec.Code != http.StatusSeeOther {
		t.Fatalf("delete = %d", rec.Code)
	}
	if notes, err := srv.store.LoadNotes(); err != nil {
		t.Fatal(err)
	} else if len(notes) != 0 {
		t.Errorf("deleted key left orphan notes: %+v", notes)
	}
}

// TestKeyNoteSurvivesRepair covers sidecar independence: repairing other
// entries preserves notes on surviving valid keys.
func TestKeyNoteSurvivesRepair(t *testing.T) {
	srv := newTestServer(t)
	sess, csrf := loginAs(t, srv)
	seedRawManifest(t, srv, `{"version":1,"users":[`+
		`{"username":"alice","enabled":true,"authorized_keys":["`+canonOf(t, fixtureEd25519)+`"]},`+
		`{"username":"legacy","enabled":true,"authorized_keys":["nope"]}]}`)
	if rec := saveNote(t, srv, sess, csrf, "alice", fixtureEd25519FP, "keep me"); rec.Code != http.StatusSeeOther {
		t.Fatalf("save = %d", rec.Code)
	}

	rec := authedPost(srv, sess, "/users/remove-invalid-keys", url.Values{csrfField: {csrf}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("repair = %d, want 303", rec.Code)
	}
	body := dashboard(t, srv, sess)
	if !strings.Contains(body, "Note: keep me") {
		t.Error("repair of other entries dropped the surviving note")
	}
}

// TestUsersPageRefinedElements covers the layout refinements: block-level
// Create action, icon+tips status, condensed fingerprints with full values
// in tooltips/details/forms, and the confirm() guard on Remove.
func TestUsersPageRefinedElements(t *testing.T) {
	srv := newTestServer(t)
	sess, _ := loginAs(t, srv)
	seedRawManifest(t, srv, `{"version":1,"users":[`+
		`{"username":"alice","enabled":true,"authorized_keys":["`+canonOf(t, fixtureEd25519)+`"]},`+
		`{"username":"frozen","enabled":false,"authorized_keys":["`+canonOf(t, fixtureRSA)+`"]}]}`)

	body := dashboard(t, srv, sess)
	if !strings.Contains(body, `<div class="form-actions"><button type="submit">Create</button></div>`) {
		t.Error("create button must sit on its own spaced row below the textarea")
	}
	for _, want := range []string{
		`aria-label="Status: enabled, new logins accepted"`,
		`title="Enabled: new logins accepted"`,
		`aria-label="Status: disabled, no new logins"`,
		`title="Disabled: no new logins"`,
		`<svg class="status-dot"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("status column must render %q", want)
		}
	}
	short := shortFingerprint(fixtureEd25519FP)
	if !strings.Contains(body, `<code title="`+fixtureEd25519FP+`">`+short+`</code>`) {
		t.Error("key must render condensed with the full fingerprint as tooltip")
	}
	if !strings.Contains(body, "<summary>Show full key</summary>") {
		t.Error("key must expose a collapsed full-key details per key")
	}
	if !strings.Contains(body, `data-confirm="Remove key `+fixtureEd25519FP+`? Removing the last key disables the user."`) {
		t.Error("remove form must carry a confirm message naming the full fingerprint with the disable warning")
	}
	if !strings.Contains(body, `onsubmit="return confirm(this.getAttribute('data-confirm'))"`) {
		t.Error("remove form must gate on a single confirm() call")
	}
	if strings.Contains(body, "😀") || strings.Contains(body, "✅") || strings.Contains(body, "❌") {
		t.Error("page must not use emojis")
	}
}

// TestRepairProblemsAndNotice covers the repair card: each problem listed
// with username + position + reason, the manifest-wide button copy, and the
// post-repair result notice for both the repaired and nothing-to-do cases.
func TestRepairProblemsAndNotice(t *testing.T) {
	srv := newTestServer(t)
	sess, csrf := loginAs(t, srv)
	seedRawManifest(t, srv, `{"version":1,"users":[`+
		`{"username":"legacy","enabled":true,"authorized_keys":["nope","also-bad","`+canonOf(t, fixtureEd25519)+`"]}]}`)

	body := dashboard(t, srv, sess)
	for _, want := range []string{
		"legacy key #1: not a valid SSH public key",
		"legacy key #2: not a valid SSH public key",
		"Remove all invalid keys (manifest-wide)",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("repair card must render %q", want)
		}
	}

	rec := authedPost(srv, sess, "/users/remove-invalid-keys", url.Values{csrfField: {csrf}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("repair = %d, want 303", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !strings.Contains(loc, "repaired=2") {
		t.Errorf("repair redirect = %q, want repaired=2", loc)
	}

	get := func(target string) string {
		r := httptest.NewRecorder()
		q := httptest.NewRequest(http.MethodGet, target, nil)
		q.AddCookie(sess)
		srv.ServeHTTP(r, q)
		if r.Code != http.StatusOK {
			t.Fatalf("GET %s = %d, want 200", target, r.Code)
		}
		return r.Body.String()
	}
	after := get(loc)
	if !strings.Contains(after, "Removed 2 invalid entries; disabled: none.") {
		t.Errorf("repair notice missing, redirect page rendered: %q", after)
	}

	// A user left with zero valid keys is named in the notice.
	seedRawManifest(t, srv, `{"version":1,"users":[{"username":"solo","enabled":true,"authorized_keys":["junk"]}]}`)
	rec = authedPost(srv, sess, "/users/remove-invalid-keys", url.Values{csrfField: {csrf}})
	loc = rec.Header().Get("Location")
	if !strings.Contains(loc, "repaired=1") || !strings.Contains(loc, "disabled=solo") {
		t.Errorf("repair redirect = %q, want repaired=1 with disabled=solo", loc)
	}
	if after := get(loc); !strings.Contains(after, "Removed 1 invalid entry; disabled: solo.") {
		t.Errorf("disabled-user notice missing: %q", after)
	}

	// Nothing invalid: notice states the no-op.
	seedRawManifest(t, srv, `{"version":1,"users":[{"username":"ok","enabled":true,"authorized_keys":["`+canonOf(t, fixtureEd25519)+`"]}]}`)
	rec = authedPost(srv, sess, "/users/remove-invalid-keys", url.Values{csrfField: {csrf}})
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "repaired=0") || strings.Contains(loc, "disabled=") {
		t.Errorf("no-op repair redirect = %q, want bare repaired=0", loc)
	} else if after := get(loc); !strings.Contains(after, "No invalid entries found.") {
		t.Errorf("no-op notice missing: %q", after)
	}
}

// TestRepairNoticeIgnoresMalformedParams covers fail-safe rendering:
// non-integer counts, invalid usernames, and a disabled list without a
// count never produce a notice.
func TestRepairNoticeIgnoresMalformedParams(t *testing.T) {
	srv := newTestServer(t)
	sess, _ := loginAs(t, srv)
	seedAlice(t, srv)

	get := func(target string) string {
		t.Helper()
		r := httptest.NewRecorder()
		q := httptest.NewRequest(http.MethodGet, target, nil)
		q.AddCookie(sess)
		srv.ServeHTTP(r, q)
		if r.Code != http.StatusOK {
			t.Fatalf("GET %s = %d, want 200", target, r.Code)
		}
		return r.Body.String()
	}
	for _, target := range []string{
		"/?repaired=abc",
		"/?repaired=-1",
		"/?repaired=1.5",
		"/?repaired=",
		"/?disabled=alice",
		"/",
	} {
		if body := get(target); strings.Contains(body, `class="notice"`) {
			t.Errorf("GET %s rendered a notice, want none", target)
		}
	}
	// Invalid usernames in the list are dropped, valid ones render escaped.
	if body := get("/?repaired=2&disabled=root"); !strings.Contains(body, "Removed 2 invalid entries; disabled: none.") {
		t.Errorf("invalid disabled name must be dropped: %q", body)
	}
	if body := get("/?repaired=1&disabled=alice"); !strings.Contains(body, "Removed 1 invalid entry; disabled: alice.") {
		t.Errorf("valid disabled name must render: %q", body)
	}
}

// TestShortFingerprint pins the condensed form: prefix plus first/last 8 of
// the hash body, with degenerate input passed through untouched.
func TestShortFingerprint(t *testing.T) {
	if got, want := shortFingerprint(fixtureEd25519FP), "SHA256:YKU2L3Tw…t/aDw4B4"; got != want {
		t.Errorf("shortFingerprint = %q, want %q", got, want)
	}
	for _, fp := range []string{"", "SHA256:abc", "(invalid)"} {
		if got := shortFingerprint(fp); got != fp {
			t.Errorf("shortFingerprint(%q) = %q, want passthrough", fp, got)
		}
	}
}
