package sftpadmin

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
)

// TestAdminLifecycleAtomic exercises the whole control plane over real HTTP:
// login, user creation with a generated keypair, single-use private key
// download, manifest secrecy, disable, and atomic manifest updates under
// concurrent readers.
func TestAdminLifecycleAtomic(t *testing.T) {
	srv := newTestServer(t)
	httpSrv := httptest.NewServer(srv)
	defer httpSrv.Close()

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := httpSrv.Client()
	client.Jar = jar

	post := func(target string, form url.Values) (int, string) {
		t.Helper()
		resp, err := client.PostForm(httpSrv.URL+target, form)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		return resp.StatusCode, string(body)
	}
	get := func(target string) (int, string) {
		t.Helper()
		resp, err := client.Get(httpSrv.URL + target)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		return resp.StatusCode, string(body)
	}
	csrfOf := func(body string) string {
		t.Helper()
		m := csrfRe.FindStringSubmatch(body)
		if m == nil {
			t.Fatal("dashboard has no csrf token")
		}
		return m[1]
	}

	// Unauthenticated dashboard shows nothing but the password prompt.
	if code, body := get("/"); code != http.StatusOK || !strings.Contains(body, "Admin password") {
		t.Fatalf("anon GET / = %d, want login prompt", code)
	}
	if code, _ := post("/login", url.Values{"password": {"wrong"}}); code != http.StatusOK {
		t.Fatalf("wrong login = %d", code)
	}
	if code, body := post("/login", url.Values{"password": {testPassword}}); code != http.StatusOK {
		t.Fatalf("good login = %d", code)
	} else {
		_ = body
	}
	code, dash := get("/")
	if code != http.StatusOK {
		t.Fatalf("dashboard = %d", code)
	}
	for _, want := range []string{`href="/"`, `href="/settings"`, ">Users<", ">Settings<", "Sign out"} {
		if !strings.Contains(dash, want) {
			t.Fatalf("dashboard nav missing %q", want)
		}
	}
	csrf := csrfOf(dash)

	// Create alice with a generated keypair.
	code, created := post("/users/create", url.Values{csrfField: {csrf}, "username": {"alice"}})
	if code != http.StatusOK || !strings.Contains(created, "one-time") {
		t.Fatalf("create = %d, want one-time handover page", code)
	}
	tok := tokenRe.FindStringSubmatch(created)
	if tok == nil {
		t.Fatal("no download token in created page")
	}
	code, first := get("/keys/download?token=" + tok[1])
	if code != http.StatusOK || !strings.Contains(first, "PRIVATE KEY") {
		t.Fatalf("first download = %d, want the private key", code)
	}
	if code, _ := get("/keys/download?token=" + tok[1]); code != http.StatusNotFound {
		t.Fatalf("second download = %d, want 404", code)
	}

	// Manifest holds version 1 + public key only; private bytes are absent.
	raw, err := os.ReadFile(srv.store.Path())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "PRIVATE KEY") || strings.Contains(string(raw), first) {
		t.Fatal("private key material present in manifest file")
	}
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("manifest does not parse: %v", err)
	}
	if m.Version != 1 || len(m.Users) != 1 || m.Users[0].Username != "alice" {
		t.Fatalf("manifest = %+v", m)
	}

	// Rapid status flips while readers parse the file: every observed
	// document must be complete and valid.
	var wg sync.WaitGroup
	stop := make(chan struct{})
	readerErr := make(chan error, 1)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				data, err := os.ReadFile(srv.store.Path())
				if err != nil {
					continue
				}
				var probe Manifest
				if err := json.Unmarshal(data, &probe); err != nil {
					select {
					case readerErr <- err:
					default:
					}
					return
				}
			}
		}()
	}
	for i := 0; i < 20; i++ {
		action := "disable"
		if i%2 == 0 {
			action = "enable"
		}
		if code, _ := post("/users/status", url.Values{
			csrfField: {csrf}, "username": {"alice"}, "action": {action},
		}); code != http.StatusOK {
			t.Fatalf("status %s = %d", action, code)
		}
	}
	if code, _ := post("/users/status", url.Values{
		csrfField: {csrf}, "username": {"alice"}, "action": {"disable"},
	}); code != http.StatusOK {
		t.Fatalf("final disable = %d", code)
	}
	close(stop)
	wg.Wait()
	select {
	case err := <-readerErr:
		t.Fatalf("reader saw partial manifest: %v", err)
	default:
	}
	final, err := srv.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if final.Users[0].Enabled {
		t.Error("alice still enabled after revoke")
	}

	// Logout ends the lifecycle: management needs login again.
	if code, _ := post("/logout", url.Values{csrfField: {csrf}}); code != http.StatusOK {
		t.Fatalf("logout = %d", code)
	}
	if code, _ := get("/"); code != http.StatusOK {
		t.Fatalf("post-logout GET / = %d", code)
	} else {
		_, body := get("/")
		if !strings.Contains(body, "Admin password") {
			t.Error("post-logout dashboard must show the login prompt")
		}
	}
}
