package sftpadmin

import (
	"path/filepath"
	"sync"
	"testing"

	"simple-fileurl/internal/config"
)

func testSharedFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	sc := config.SharedConfig{
		HashAlgorithm:     "md5",
		HashTarget:        "file",
		PublicURL:         "https://example.test",
		AdminToken:        "tok",
		SftpAdminPassword: "pw",
	}
	if err := sc.Save(path); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSettingsRegistryUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, def := range SettingsRegistry {
		if def.Key == "" || def.Service == "" || def.Description == "" || def.AllowedValues == "" {
			t.Fatalf("incomplete definition: %+v", def)
		}
		if seen[def.Key] {
			t.Fatalf("duplicate key %q", def.Key)
		}
		seen[def.Key] = true
	}
	if len(SettingsRegistry) != 5 {
		t.Fatalf("registry has %d entries, want 5", len(SettingsRegistry))
	}
}

func TestGetSettings(t *testing.T) {
	path := testSharedFile(t)
	got, err := GetSettings(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(SettingsRegistry) {
		t.Fatalf("got %d settings, want %d", len(got), len(SettingsRegistry))
	}
	want := map[string]string{
		"HASH_ALGORITHM": "md5", "HASH_TARGET": "file",
		"PUBLIC_URL": "https://example.test", "ADMIN_TOKEN": "tok",
		"SFTP_ADMIN_PASSWORD": "pw",
	}
	for _, s := range got {
		if want[s.Key] != s.Value {
			t.Fatalf("%s = %q, want %q", s.Key, s.Value, want[s.Key])
		}
	}
	if _, err := GetSettings(filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Fatal("missing file: expected error")
	}
}

func TestApplySetting(t *testing.T) {
	path := testSharedFile(t)
	if err := ApplySetting(path, "HASH_ALGORITHM", "sha256"); err != nil {
		t.Fatal(err)
	}
	got, err := GetSettings(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range got {
		if s.Key == "HASH_ALGORITHM" && s.Value != "sha256" {
			t.Fatalf("not applied: %+v", s)
		}
	}
	// Invalid value leaves the file untouched.
	if err := ApplySetting(path, "HASH_ALGORITHM", "sha1"); err == nil {
		t.Fatal("invalid value: expected error")
	}
	// Unknown key leaves the file untouched.
	if err := ApplySetting(path, "NOPE", "x"); err == nil {
		t.Fatal("unknown key: expected error")
	}
	after, err := GetSettings(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range after {
		if s.Key == "HASH_ALGORITHM" && s.Value != "sha256" {
			t.Fatalf("rejected write changed the file: %+v", s)
		}
	}
	// Missing file errors instead of inventing defaults.
	if err := ApplySetting(filepath.Join(t.TempDir(), "missing.json"), "HASH_ALGORITHM", "md5"); err == nil {
		t.Fatal("missing file: expected error")
	}
}

func TestApplySettingsAllOrNothing(t *testing.T) {
	path := testSharedFile(t)
	errs, err := ApplySettings(path, map[string]string{
		"PUBLIC_URL":     "https://new.test",
		"HASH_ALGORITHM": "sha1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, bad := errs["HASH_ALGORITHM"]; !bad {
		t.Fatalf("errs = %v, want HASH_ALGORITHM rejected", errs)
	}
	got, err := GetSettings(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range got {
		if s.Key == "PUBLIC_URL" && s.Value != "https://example.test" {
			t.Fatalf("valid field persisted despite sibling error: %+v", s)
		}
		if s.Key == "HASH_ALGORITHM" && s.Value != "md5" {
			t.Fatalf("invalid field persisted: %+v", s)
		}
	}
}

func TestApplySettingsUnknownKeyRejected(t *testing.T) {
	path := testSharedFile(t)
	errs, err := ApplySettings(path, map[string]string{"NOPE": "x"})
	if err != nil {
		t.Fatal(err)
	}
	if _, bad := errs["NOPE"]; !bad {
		t.Fatalf("errs = %v, want NOPE rejected", errs)
	}
	if _, err := GetSettings(path); err != nil {
		t.Fatal(err)
	}
}

func TestApplySettingsEmptySecretKeepsValue(t *testing.T) {
	path := testSharedFile(t)
	errs, err := ApplySettings(path, map[string]string{
		"ADMIN_TOKEN": "",
		"PUBLIC_URL":  "https://new.test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(errs) != 0 {
		t.Fatalf("errs = %v, want none", errs)
	}
	sc, err := config.LoadShared(path)
	if err != nil {
		t.Fatal(err)
	}
	if sc.AdminToken != "tok" || sc.PublicURL != "https://new.test" {
		t.Fatalf("secret kept + url saved: %+v", sc)
	}
}

func TestApplySettingsConcurrentDistinctFieldsKeepBoth(t *testing.T) {
	path := testSharedFile(t)
	const n = 50
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < n; i++ {
			if _, err := ApplySettings(path, map[string]string{"PUBLIC_URL": "https://kept.test"}); err != nil {
				t.Error(err)
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < n; i++ {
			algo := "md5"
			if i%2 == 0 {
				algo = "sha256"
			}
			if _, err := ApplySettings(path, map[string]string{"HASH_ALGORITHM": algo}); err != nil {
				t.Error(err)
				return
			}
		}
	}()
	wg.Wait()
	sc, err := config.LoadShared(path)
	if err != nil {
		t.Fatalf("concurrent saves left an unreadable file: %v", err)
	}
	if sc.PublicURL != "https://kept.test" {
		t.Fatalf("PUBLIC_URL = %q, want kept value: concurrent save lost a field", sc.PublicURL)
	}
	if sc.HashAlgorithm != "md5" && sc.HashAlgorithm != "sha256" {
		t.Fatalf("HASH_ALGORITHM = %q, want a fully applied submission", sc.HashAlgorithm)
	}
}
