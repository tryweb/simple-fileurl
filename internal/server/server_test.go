package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"simple-fileurl/internal/config"
	"simple-fileurl/internal/store"
)

func testServer(t *testing.T, target, algo string) (*Server, config.Config) {
	t.Helper()
	ns := t.TempDir()
	if err := os.MkdirAll(filepath.Join(ns, "files", "mydir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ns, "files", "mydir", "cron.txt"), []byte("cron-body"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ns, "files", "README.txt"), []byte("readme-body"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{
		ContainerRoot: ns,
		SharePrefix:   "files",
		PublicURL:     "https://example.test",
		HashTarget:    target,
		HashAlgorithm: algo,
		Port:          "8080",
	}
	return New(cfg, store.New(cfg)), cfg
}

func TestHealth(t *testing.T) {
	srv, _ := testServer(t, "file", "md5")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "ok") {
		t.Fatalf("health: %d %q", rec.Code, rec.Body.String())
	}
}

func TestHealthMissingNamespace(t *testing.T) {
	cfg := config.Config{
		ContainerRoot: t.TempDir(),
		SharePrefix:   "files",
		PublicURL:     "https://example.test",
		HashTarget:    "file",
		HashAlgorithm: "md5",
		Port:          "8080",
	}
	srv := New(cfg, store.New(cfg))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("health: %d", rec.Code)
	}
}

func TestDownloadRoundTrip(t *testing.T) {
	for _, target := range []string{"file", "filename"} {
		for _, algo := range []string{"md5", "sha256"} {
			srv, cfg := testServer(t, target, algo)
			// Get hashes from the listing page.
			rec := httptest.NewRecorder()
			srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("%s/%s index: %d", target, algo, rec.Code)
			}
			body := rec.Body.String()
			idx := strings.Index(body, cfg.PublicURL+"/")
			if idx < 0 {
				t.Fatalf("%s/%s: no share URL in index", target, algo)
			}
			rest := body[idx+len(cfg.PublicURL)+1:]
			end := strings.IndexAny(rest, `"< `)
			link := rest[:end]
			parts := strings.Split(link, "/")
			if len(parts) != 2 {
				t.Fatalf("%s/%s: bad link %q", target, algo, link)
			}
			drec := httptest.NewRecorder()
			dreq := httptest.NewRequest(http.MethodGet, "/"+link, nil)
			srv.ServeHTTP(drec, dreq)
			if drec.Code != http.StatusOK {
				t.Fatalf("%s/%s download: %d", target, algo, drec.Code)
			}
			got, _ := io.ReadAll(drec.Result().Body)
			if !strings.Contains(string(got), "-body") {
				t.Fatalf("%s/%s body: %q", target, algo, got)
			}
			if cd := drec.Header().Get("Content-Disposition"); !strings.Contains(cd, "attachment") {
				t.Fatalf("%s/%s disposition: %q", target, algo, cd)
			}
		}
	}
}

func TestDownloadOutcomes(t *testing.T) {
	srv, _ := testServer(t, "file", "md5")
	cases := []struct {
		path string
		want int
	}{
		{"/" + strings.Repeat("a", 32) + "/" + strings.Repeat("b", 32), http.StatusNotFound},
		{"/short/" + strings.Repeat("b", 32), http.StatusBadRequest},
		{"/" + strings.Repeat("a", 64) + "/" + strings.Repeat("b", 64), http.StatusBadRequest},
		{"/onlyone", http.StatusNotFound},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, c.path, nil))
		if rec.Code != c.want {
			t.Fatalf("%s: got %d want %d (%q)", c.path, rec.Code, c.want, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "/tmp") || strings.Contains(rec.Body.String(), "files/") {
			t.Fatalf("%s: response leaks path: %q", c.path, rec.Body.String())
		}
	}
}

func TestIndexEscapesAndEmpty(t *testing.T) {
	ns := t.TempDir()
	if err := os.MkdirAll(filepath.Join(ns, "files"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ns, "files", `<evil>&".txt`), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{
		ContainerRoot: ns,
		SharePrefix:   "files",
		PublicURL:     "https://example.test",
		HashTarget:    "filename",
		HashAlgorithm: "md5",
		Port:          "8080",
	}
	srv := New(cfg, store.New(cfg))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("index: %d", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, `<evil>&".txt`) {
		t.Fatalf("unescaped filename in index: %q", body)
	}
	if !strings.Contains(body, "Hash target: filename") {
		t.Fatalf("config not shown: %q", body)
	}

	empty := t.TempDir()
	if err := os.MkdirAll(filepath.Join(empty, "files"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg.ContainerRoot = empty
	srv = New(cfg, store.New(cfg))
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "No files available") {
		t.Fatalf("empty index: %d %q", rec.Code, rec.Body.String())
	}
}

func TestDownloadHostileFilename(t *testing.T) {
	ns := t.TempDir()
	if err := os.MkdirAll(filepath.Join(ns, "files"), 0o755); err != nil {
		t.Fatal(err)
	}
	hostile := "a\"b\\c\nd.txt"
	if err := os.WriteFile(filepath.Join(ns, "files", hostile), []byte("hostile-body"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{
		ContainerRoot: ns,
		SharePrefix:   "files",
		PublicURL:     "https://example.test",
		HashTarget:    "filename",
		HashAlgorithm: "md5",
		Port:          "8080",
	}
	st := store.New(cfg)
	entries, err := st.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries: %+v", entries)
	}
	srv := New(cfg, st)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/"+entries[0].DirHash+"/"+entries[0].FileHash, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("download: %d", rec.Code)
	}
	if cd := rec.Result().Header.Get("Content-Disposition"); cd != `attachment; filename="a_b_c_d.txt"` {
		t.Fatalf("disposition not neutralized: %q", cd)
	}
}

func TestSafeFilename(t *testing.T) {
	if got := safeFilename("cron.txt"); got != `"cron.txt"` {
		t.Fatalf("plain: %q", got)
	}
	if got := safeFilename("Emma,Ron_(Jonathan).txt"); got != `"Emma,Ron_(Jonathan).txt"` {
		t.Fatalf("printable: %q", got)
	}
	if got := safeFilename(""); got != `"download"` {
		t.Fatalf("empty: %q", got)
	}
}
