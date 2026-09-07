package sftpadmin

import (
	"html"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// overlayWants is the markup contract both generate paths share: a zero-JS
// focused overlay dialog with the username, the new fingerprint, the
// one-time warning, an autofocused Download link, and a Close anchor to /.
var overlayWants = []string{
	`<div class="overlay">`,
	`<dialog class="card overlay-card" open`,
	`aria-labelledby="generated-title"`,
	`id="generated-title"`,
	"Download private key (one-time)",
	"autofocus",
	`<a class="btn secondary" href="/">Close</a>`,
	"The private key below can be downloaded exactly once. It is never stored. Save it now.",
}

func assertOverlayResult(t *testing.T, body, username, fingerprint string) {
	t.Helper()
	got := html.UnescapeString(body)
	for _, want := range overlayWants {
		if !strings.Contains(got, want) {
			t.Errorf("generate overlay must render %q", want)
		}
	}
	for _, want := range []string{
		"Key generated for " + username,
		fingerprint,
		"<table",
		"Existing users",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generate overlay must render %q", want)
		}
	}
	if m := tokenRe.FindStringSubmatch(got); m == nil {
		t.Error("generate overlay must link the one-time download")
	}
	if strings.Contains(got, "Back to users") {
		t.Error("generate overlay must not keep the old separate-page Back to users link")
	}
}

func assertNoOverlay(t *testing.T, body string) {
	t.Helper()
	for _, want := range []string{`class="overlay"`, "<dialog", "Key generated for"} {
		if strings.Contains(body, want) {
			t.Errorf("response must show no result card, found %q", want)
		}
	}
	if m := tokenRe.FindStringSubmatch(body); m != nil {
		t.Error("response must carry no download token")
	}
}

func assertErrorOverlay(t *testing.T, body, title string) {
	t.Helper()
	got := html.UnescapeString(body)
	for _, want := range []string{
		`<div class="overlay">`,
		`<dialog class="card overlay-card" open`,
		`aria-labelledby="feedback-title"`,
		`id="feedback-title"`,
		title,
		`<a class="btn secondary" autofocus href="/">Close</a>`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("error overlay must render %q", want)
		}
	}
	if strings.Contains(got, "Key generated for") {
		t.Error("error overlay must not render a generated result")
	}
	if m := tokenRe.FindStringSubmatch(got); m != nil {
		t.Error("error overlay must carry no download token")
	}
}

// TestCreateGenerateRendersOverlayResult covers the unified flow:
// create-user with an empty key returns 200 with the same overlay dialog
// as row generation, over a dashboard that already lists the new user.
func TestCreateGenerateRendersOverlayResult(t *testing.T) {
	srv := newTestServer(t)
	srv.gen = stubGen(fixtureEd25519)
	sess, csrf := loginAs(t, srv)

	rec := createUser(t, srv, sess, csrf, "alice", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("create-generate = %d, want 200 with the overlay result", rec.Code)
	}
	assertOverlayResult(t, rec.Body.String(), "alice", fixtureEd25519FP)
	if got := html.UnescapeString(rec.Body.String()); !strings.Contains(got, "alice") {
		t.Error("dashboard behind the overlay must list the new user")
	}
	if loc := rec.Header().Get("Location"); loc != "" {
		t.Errorf("create-generate must render via 200 body, got redirect to %q", loc)
	}
	if keys, _ := mustKeys(t, srv, "alice"); len(keys) != 1 {
		t.Fatalf("alice keys = %d, want 1 stored public key", len(keys))
	}
}

// TestCreatePasteStillRedirects pins the unchanged path: a pasted key
// stays 303 to / with no overlay and no token in the body.
func TestCreatePasteStillRedirects(t *testing.T) {
	srv := newTestServer(t)
	sess, csrf := loginAs(t, srv)

	rec := createUser(t, srv, sess, csrf, "bob", fixtureRSA)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("create-paste = %d, want 303", rec.Code)
	}
	assertNoOverlay(t, rec.Body.String())
}

// TestRowGenerateRendersOverlayMarkup pins the row path to the same
// overlay markup: dialog, autofocused download, Close to /, dashboard
// table with the pre-existing key still present.
func TestRowGenerateRendersOverlayMarkup(t *testing.T) {
	srv := newTestServer(t)
	srv.gen = stubGen(fixtureEd25519)
	sess, csrf := loginAs(t, srv)
	seedRawManifest(t, srv, `{"version":1,"users":[{"username":"alice","enabled":true,"authorized_keys":["`+canonOf(t, fixtureRSA)+`"]}]}`)

	rec := authedPost(srv, sess, "/users/generate-key", url.Values{
		csrfField: {csrf}, "username": {"alice"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("row generate = %d, want 200 with the overlay result", rec.Code)
	}
	assertOverlayResult(t, rec.Body.String(), "alice", fixtureEd25519FP)
	if got := html.UnescapeString(rec.Body.String()); !strings.Contains(got, fixtureRSAFP) {
		t.Error("dashboard behind the overlay must keep the pre-existing key")
	}
	if loc := rec.Header().Get("Location"); loc != "" {
		t.Errorf("row generate must render via 200 body, got redirect to %q", loc)
	}
}

// TestPlainDashboardShowsNoOverlay pins the dismiss path: plain GET /
// renders no overlay, no dialog, and no token.
func TestPlainDashboardShowsNoOverlay(t *testing.T) {
	srv := newTestServer(t)
	sess, _ := loginAs(t, srv)
	seedRawManifest(t, srv, `{"version":1,"users":[{"username":"alice","enabled":true,"authorized_keys":["`+canonOf(t, fixtureRSA)+`"]}]}`)

	assertNoOverlay(t, dashboard(t, srv, sess))
}

func TestStatusErrorRendersFeedbackOverlay(t *testing.T) {
	srv := newTestServer(t)
	sess, csrf := loginAs(t, srv)
	seedRawManifest(t, srv, `{"version":1,"users":[{"username":"frozen","enabled":false,"authorized_keys":[]}]}`)

	rec := authedPost(srv, sess, "/users/status", url.Values{
		csrfField: {csrf}, "username": {"frozen"}, "action": {"enable"},
	})
	if rec.Code != http.StatusConflict {
		t.Fatalf("enable without usable keys = %d, want 409", rec.Code)
	}
	assertErrorOverlay(t, rec.Body.String(), "Could not update user status")
	if !strings.Contains(rec.Body.String(), "cannot enable a user with no usable keys") {
		t.Error("status error overlay must explain why enable failed")
	}
}

func TestUserMutationErrorsRenderFeedbackOverlay(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		form    func(csrf string) url.Values
		setup   string
		status  int
		title   string
		message string
	}{
		{
			name: "unknown status action",
			path: "/users/status",
			form: func(csrf string) url.Values {
				return url.Values{csrfField: {csrf}, "username": {"alice"}, "action": {"pause"}}
			},
			setup:   `{"version":1,"users":[{"username":"alice","enabled":true,"authorized_keys":[]}]}`,
			status:  http.StatusBadRequest,
			title:   "Could not update user status",
			message: "unknown action",
		},
		{
			name: "delete enabled user",
			path: "/users/delete",
			form: func(csrf string) url.Values {
				return url.Values{csrfField: {csrf}, "username": {"alice"}}
			},
			setup:   `{"version":1,"users":[{"username":"alice","enabled":true,"authorized_keys":[]}]}`,
			status:  http.StatusConflict,
			title:   "Could not delete user",
			message: "disable the user before deletion",
		},
		{
			name: "invalid delete fingerprint",
			path: "/users/delete-key",
			form: func(csrf string) url.Values {
				return url.Values{csrfField: {csrf}, "username": {"alice"}, "fingerprint": {"bad"}}
			},
			setup:   `{"version":1,"users":[{"username":"alice","enabled":true,"authorized_keys":[]}]}`,
			status:  http.StatusBadRequest,
			title:   "Could not remove key",
			message: "invalid fingerprint: want canonical SHA256 form",
		},
		{
			name: "overlong key note",
			path: "/users/key-note",
			form: func(csrf string) url.Values {
				return url.Values{csrfField: {csrf}, "username": {"alice"}, "fingerprint": {fixtureEd25519FP}, "note": {strings.Repeat("x", MaxKeyNoteLength+1)}}
			},
			setup:   `{"version":1,"users":[{"username":"alice","enabled":true,"authorized_keys":["` + canonOf(t, fixtureEd25519) + `"]}]}`,
			status:  http.StatusBadRequest,
			title:   "Could not save key note",
			message: "note too long: max 120 characters",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newTestServer(t)
			sess, csrf := loginAs(t, srv)
			seedRawManifest(t, srv, tt.setup)

			rec := authedPost(srv, sess, tt.path, tt.form(csrf))
			if rec.Code != tt.status {
				t.Fatalf("%s = %d, want %d", tt.path, rec.Code, tt.status)
			}
			assertErrorOverlay(t, rec.Body.String(), tt.title)
			if !strings.Contains(rec.Body.String(), tt.message) {
				t.Errorf("error overlay must include %q", tt.message)
			}
		})
	}
}

func TestFailedGenerationsShowErrorOverlay(t *testing.T) {
	t.Run("create-generate failure is 500 with error overlay", func(t *testing.T) {
		srv := newTestServer(t)
		srv.gen = func(string) (KeyPair, error) { return KeyPair{}, errTestBoom }
		sess, csrf := loginAs(t, srv)
		rec := createUser(t, srv, sess, csrf, "alice", "")
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("failed create-generate = %d, want 500", rec.Code)
		}
		assertErrorOverlay(t, rec.Body.String(), "Could not create user")
		if !strings.Contains(rec.Body.String(), "key generation failed") {
			t.Error("create-generate error overlay must explain the failure")
		}
		if m, err := srv.store.Load(); err != nil || len(m.Users) != 0 {
			t.Errorf("failed create-generate mutated the manifest: %+v, err=%v", m, err)
		}
	})

	t.Run("row generate failure is 500 with error overlay", func(t *testing.T) {
		srv := newTestServer(t)
		srv.gen = func(string) (KeyPair, error) { return KeyPair{}, errTestBoom }
		sess, csrf := loginAs(t, srv)
		seedRawManifest(t, srv, `{"version":1,"users":[{"username":"alice","enabled":true,"authorized_keys":[]}]}`)
		rec := authedPost(srv, sess, "/users/generate-key", url.Values{
			csrfField: {csrf}, "username": {"alice"},
		})
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("failed row generate = %d, want 500", rec.Code)
		}
		assertErrorOverlay(t, rec.Body.String(), "Could not generate key")
		if !strings.Contains(rec.Body.String(), "key generation failed") {
			t.Error("row generate error overlay must explain the failure")
		}
	})

	t.Run("unknown user and bad input carry error overlays", func(t *testing.T) {
		srv := newTestServer(t)
		srv.gen = stubGen(fixtureEd25519)
		sess, csrf := loginAs(t, srv)
		for name, tc := range map[string]struct {
			target   string
			username string
			want     int
		}{
			"row generate unknown user": {"generate-key", "ghost", http.StatusNotFound},
			"row generate bad username": {"generate-key", "Bad Name!", http.StatusBadRequest},
			"create bad username":       {"create", "Bad Name!", http.StatusBadRequest},
		} {
			rec := authedPost(srv, sess, "/users/"+tc.target, url.Values{
				csrfField: {csrf}, "username": {tc.username},
			})
			if rec.Code != tc.want {
				t.Errorf("%s = %d, want %d", name, rec.Code, tc.want)
			}
			if tc.target == "generate-key" {
				assertErrorOverlay(t, rec.Body.String(), "Could not generate key")
			} else {
				assertErrorOverlay(t, rec.Body.String(), "Could not create user")
			}
		}
	})
}
