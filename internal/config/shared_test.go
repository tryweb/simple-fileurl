package config

import (
	"os"
	"path/filepath"
	"testing"
)

func testShared() SharedConfig {
	return SharedConfig{
		HashAlgorithm:     "sha256",
		HashTarget:        "file",
		PublicURL:         "https://example.test",
		AdminToken:        "tok",
		SftpAdminPassword: "pw",
	}
}

func TestSharedRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	want := testShared()
	if err := want.Save(path); err != nil {
		t.Fatal(err)
	}
	got, err := LoadShared(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("round trip: %+v", got)
	}
	// The non-root file-sharing process must be able to read the file.
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm()&0o044 == 0 {
		t.Fatalf("config file not world/group-readable: %v", st.Mode())
	}
}

func TestSharedMissingFile(t *testing.T) {
	if _, err := LoadShared(filepath.Join(t.TempDir(), "nope.json")); !os.IsNotExist(err) {
		t.Fatalf("missing file: %v", err)
	}
}

func TestSharedCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte("{oops"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadShared(path); err == nil {
		t.Fatal("corrupt file: expected error")
	}
}

func TestSharedValidation(t *testing.T) {
	bad := []SharedConfig{
		func() SharedConfig { c := testShared(); c.HashTarget = "x"; return c }(),
		func() SharedConfig { c := testShared(); c.HashAlgorithm = "sha1"; return c }(),
		func() SharedConfig { c := testShared(); c.PublicURL = ""; return c }(),
		func() SharedConfig { c := testShared(); c.AdminToken = ""; return c }(),
		func() SharedConfig { c := testShared(); c.SftpAdminPassword = ""; return c }(),
	}
	for i, c := range bad {
		if err := c.Validate(); err == nil {
			t.Fatalf("case %d: expected error", i)
		}
		if err := c.Save(filepath.Join(t.TempDir(), "c.json")); err == nil {
			t.Fatalf("case %d: save accepted invalid config", i)
		}
	}
}

func TestSharedFilePermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := testShared().Save(path); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := st.Mode().Perm(); perm != 0o640 {
		t.Fatalf("config file mode = %o, want 640", perm)
	}
	if leftovers, _ := filepath.Glob(filepath.Join(dir, "config-*.tmp")); len(leftovers) != 0 {
		t.Errorf("leftover temp files: %v", leftovers)
	}
}
