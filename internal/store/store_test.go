package store

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"simple-fileurl/internal/config"
)

func testConfig(root, target, algo string) config.Config {
	return config.Config{
		ContainerRoot: root,
		SharePrefix:   "files",
		PublicURL:     "https://example.test",
		HashTarget:    target,
		HashAlgorithm: algo,
		Port:          "8080",
	}
}

// fixture builds <tmp>/ns/files/{mydir/cron.txt, README.txt}.
func fixture(t *testing.T) string {
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
	return ns
}

func TestListLogicalPaths(t *testing.T) {
	ns := fixture(t)
	st := New(testConfig(ns, "filename", "md5"))
	entries, err := st.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries: %+v", entries)
	}
	if entries[0].LogicalPath != "files/README.txt" || entries[1].LogicalPath != "files/mydir/cron.txt" {
		t.Fatalf("logical paths: %+v", entries)
	}
	if entries[1].LogicalDir != "files/mydir" {
		t.Fatalf("logical dir: %+v", entries[1])
	}
}

func TestResolveRoundTrip(t *testing.T) {
	for _, target := range []string{"file", "filename"} {
		ns := fixture(t)
		st := New(testConfig(ns, target, "md5"))
		entries, err := st.List()
		if err != nil {
			t.Fatal(err)
		}
		var want *Entry
		for i := range entries {
			if entries[i].LogicalPath == "files/mydir/cron.txt" {
				want = &entries[i]
			}
		}
		if want == nil {
			t.Fatal("cron.txt not listed")
		}
		abs, got, err := st.Resolve(want.DirHash, want.FileHash)
		if err != nil {
			t.Fatalf("%s: resolve: %v", target, err)
		}
		body, err := os.ReadFile(abs)
		if err != nil {
			t.Fatal(err)
		}
		if string(body) != "cron-body" || got.LogicalPath != want.LogicalPath {
			t.Fatalf("%s: resolved %+v body %q", target, got, body)
		}
	}
}

func TestResolveRejectsBadInput(t *testing.T) {
	ns := fixture(t)
	st := New(testConfig(ns, "file", "md5"))
	if _, _, err := st.Resolve("nothex!!", strings.Repeat("a", 32)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("malformed dir hash: %v", err)
	}
	// sha256-length digest rejected under md5 config.
	if _, _, err := st.Resolve(strings.Repeat("a", 64), strings.Repeat("b", 64)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("wrong-length digest: %v", err)
	}
	if _, _, err := st.Resolve(strings.Repeat("a", 32), strings.Repeat("b", 32)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown hash: %v", err)
	}
}

func TestSymlinkExcluded(t *testing.T) {
	ns := fixture(t)
	// Symlink inside the namespace pointing outside must not be served.
	outside := filepath.Join(ns, "secret.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(ns, "files", "evil.txt")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	st := New(testConfig(ns, "filename", "md5"))
	entries, err := st.List()
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Contains(e.LogicalPath, "evil") {
			t.Fatalf("symlink listed: %+v", e)
		}
	}
}

func TestContentCollisionIsConflict(t *testing.T) {
	ns := t.TempDir()
	if err := os.MkdirAll(filepath.Join(ns, "files", "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Two files with identical content in the same directory share the same
	// content hash under HASH_TARGET=file: resolving must not serve either.
	for _, name := range []string{"x.txt", "y.txt"} {
		if err := os.WriteFile(filepath.Join(ns, "files", "a", name), []byte("same"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	st := New(testConfig(ns, "file", "md5"))
	entries, err := st.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries: %+v", entries)
	}
	if entries[0].FileHash != entries[1].FileHash {
		t.Fatalf("expected identical content hashes: %+v", entries)
	}
	if _, _, err := st.Resolve(entries[0].DirHash, entries[0].FileHash); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected conflict, got %v", err)
	}
}

func TestOutsideNamespaceNotServed(t *testing.T) {
	ns := fixture(t)
	st := New(testConfig(ns, "file", "md5"))
	if err := st.Check(); err != nil {
		t.Fatalf("check: %v", err)
	}
	bad := New(config.Config{
		ContainerRoot: ns,
		SharePrefix:   "missing",
		PublicURL:     "https://example.test",
		HashTarget:    "file",
		HashAlgorithm: "md5",
		Port:          "8080",
	})
	if err := bad.Check(); err == nil {
		t.Fatal("expected error for missing namespace")
	}
}
