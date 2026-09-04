package config

import (
	"strings"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	cfg, err := loadFromEnv(func(string) string { return "" })
	if err == nil {
		t.Fatalf("expected error for missing SHARE_PREFIX, got %+v", cfg)
	}
}

func TestLoadValid(t *testing.T) {
	env := map[string]string{
		"SHARE_ROOT":     "/opt/sharefiles",
		"SHARE_PREFIX":   "files",
		"PUBLIC_URL":     "https://weurl.everplast.net/",
		"HASH_TARGET":    "file",
		"HASH_ALGORITHM": "md5",
		"PORT":           "8080",
		"ADMIN_PATH":     "admin123456",
		"ADMIN_PASSWORD": "s3cret!",
	}
	cfg, err := loadFromEnv(func(k string) string { return env[k] })
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.AdminPath != "admin123456" || cfg.AdminPassword != "s3cret!" {
		t.Fatalf("admin config not loaded: %+v", cfg)
	}
	if cfg.ShareURL("abc", "def") != "https://weurl.everplast.net/abc/def" {
		t.Fatalf("ShareURL not normalized: %q", cfg.ShareURL("abc", "def"))
	}
	if cfg.NamespaceRoot() != "/opt/sharefiles/files" {
		t.Fatalf("NamespaceRoot: %q", cfg.NamespaceRoot())
	}
}

func TestLoadRejectsBadValues(t *testing.T) {
	base := map[string]string{
		"SHARE_ROOT":     "/opt/sharefiles",
		"SHARE_PREFIX":   "files",
		"PUBLIC_URL":     "https://example.test",
		"HASH_TARGET":    "file",
		"HASH_ALGORITHM": "md5",
		"PORT":           "8080",
	}
	cases := map[string]map[string]string{
		"bad target":     {"HASH_TARGET": "random"},
		"bad algorithm":  {"HASH_ALGORITHM": "sha1"},
		"bad prefix":     {"SHARE_PREFIX": "../etc"},
		"abs prefix":     {"SHARE_PREFIX": "/files"},
		"empty prefix":   {"SHARE_PREFIX": ""},
		"bad port":       {"PORT": "http"},
		"short admin":    {"ADMIN_PATH": "short"},
		"admin slash":    {"ADMIN_PATH": "a/b/cdefgh"},
		"admin back":     {"ADMIN_PATH": `a\bcd1234`},
		"admin reserved": {"ADMIN_PATH": "healthz"},
		"admin space":    {"ADMIN_PATH": "has space1"},
	}
	for name, override := range cases {
		env := map[string]string{}
		for k, v := range base {
			env[k] = v
		}
		for k, v := range override {
			env[k] = v
		}
		if _, err := loadFromEnv(func(k string) string { return env[k] }); err == nil {
			t.Fatalf("%s: expected error", name)
		}
	}
}

func TestMissingRootReported(t *testing.T) {
	cfg := Config{
		ContainerRoot: "/opt/sharefiles",
		SharePrefix:   "files",
		PublicURL:     "https://example.test",
		HashTarget:    "file",
		HashAlgorithm: "md5",
		Port:          "8080",
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
	if !strings.HasPrefix(cfg.NamespaceRoot(), "/opt/sharefiles/files") {
		t.Fatalf("NamespaceRoot: %q", cfg.NamespaceRoot())
	}
}

func TestValidateAdminPath(t *testing.T) {
	valid := []string{"", "admin123456", "x9-_.~abc"}
	for _, p := range valid {
		if err := ValidateAdminPath(p); err != nil {
			t.Fatalf("%q: unexpected error %v", p, err)
		}
	}
	invalid := []string{
		"short",
		"a/b/cdefgh",
		`a\bcd1234`,
		"healthz",
		".",
		"..",
		"has space1",
		"trailing!",
		strings.Repeat("a", 129),
	}
	for _, p := range invalid {
		if err := ValidateAdminPath(p); err == nil {
			t.Fatalf("%q: expected error", p)
		}
	}
}
