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
	}
	cfg, err := loadFromEnv(func(k string) string { return env[k] })
	if err != nil {
		t.Fatalf("Load: %v", err)
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
		"bad target":    {"HASH_TARGET": "random"},
		"bad algorithm": {"HASH_ALGORITHM": "sha1"},
		"bad prefix":    {"SHARE_PREFIX": "../etc"},
		"abs prefix":    {"SHARE_PREFIX": "/files"},
		"empty prefix":  {"SHARE_PREFIX": ""},
		"bad port":      {"PORT": "http"},
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
