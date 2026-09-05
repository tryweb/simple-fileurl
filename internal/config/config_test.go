package config

import (
	"os"
	"path/filepath"
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
		"ADMIN_TOKEN":    "link-admin-token",
		"LINKS_DIR":      "/var/lib/file-links",
		"WEB_GID":        "2001",
	}
	cfg, err := loadFromEnv(func(k string) string { return env[k] })
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.AdminToken != "link-admin-token" || cfg.LinksDir != "/var/lib/file-links" {
		t.Fatalf("link config not loaded: %+v", cfg)
	}
	if cfg.ShareURL("abc", "def") != "https://weurl.everplast.net/abc/def" {
		t.Fatalf("ShareURL not normalized: %q", cfg.ShareURL("abc", "def"))
	}
	if cfg.NamespaceRoot() != "/opt/sharefiles" {
		t.Fatalf("NamespaceRoot: %q", cfg.NamespaceRoot())
	}
	if cfg.WebGID != "2001" {
		t.Fatalf("WebGID: %q", cfg.WebGID)
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
		"ADMIN_TOKEN":    "link-admin-token",
	}
	cases := map[string]map[string]string{
		"bad target":    {"HASH_TARGET": "random"},
		"bad algorithm": {"HASH_ALGORITHM": "sha1"},
		"bad prefix":    {"SHARE_PREFIX": "../etc"},
		"abs prefix":    {"SHARE_PREFIX": "/files"},
		"empty prefix":  {"SHARE_PREFIX": ""},
		"bad port":      {"PORT": "http"},
		"missing token": {"ADMIN_TOKEN": ""},
		"rel links":     {"LINKS_DIR": "relative/links"},
		"bad web gid":   {"WEB_GID": "notanumber"},
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
		AdminToken:    "link-admin-token",
		LinksDir:      "/var/lib/file-links",
		WebGID:        "2001",
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
	if !strings.HasPrefix(cfg.NamespaceRoot(), "/opt/sharefiles") {
		t.Fatalf("NamespaceRoot: %q", cfg.NamespaceRoot())
	}
}

func TestIsEligibleNamespace(t *testing.T) {
	cfg := Config{
		ContainerRoot: "/opt/sharefiles",
		SharePrefix:   "files",
		PublicURL:     "https://example.test",
		HashTarget:    "file",
		HashAlgorithm: "md5",
		Port:          "8080",
		WebGID:        "2001",
	}
	eligible := []string{"files", "alice", "jonathan", "bob_1", "a-b-c", "_svc"}
	for _, name := range eligible {
		if !cfg.IsEligibleNamespace(name) {
			t.Fatalf("%q: expected eligible", name)
		}
	}
	ineligible := []string{"", ".", "..", "TempData", "UPPER", "temp.data", "a/b", `a\b`, ".hidden", "9lives"}
	for _, name := range ineligible {
		if cfg.IsEligibleNamespace(name) {
			t.Fatalf("%q: expected ineligible", name)
		}
	}
}

func TestCheckFilesystem(t *testing.T) {
	root := t.TempDir()
	cfg := Config{
		ContainerRoot: root,
		SharePrefix:   "files",
		PublicURL:     "https://example.test",
		HashTarget:    "file",
		HashAlgorithm: "md5",
		Port:          "8080",
		WebGID:        "2001",
	}
	// Missing prefix directory fails startup.
	if err := cfg.CheckFilesystem(); err == nil {
		t.Fatal("expected error when prefix dir is missing")
	}
	// Prefix-only root succeeds: per-user directories are optional.
	if err := os.MkdirAll(filepath.Join(root, "files"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := cfg.CheckFilesystem(); err != nil {
		t.Fatalf("prefix-only root rejected: %v", err)
	}
	// Adding a per-user directory keeps startup green.
	if err := os.MkdirAll(filepath.Join(root, "alice"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := cfg.CheckFilesystem(); err != nil {
		t.Fatalf("root with per-user dir rejected: %v", err)
	}
}

func TestLinksDirDefault(t *testing.T) {
	env := map[string]string{
		"SHARE_ROOT":     "/opt/sharefiles",
		"SHARE_PREFIX":   "files",
		"PUBLIC_URL":     "https://example.test",
		"HASH_TARGET":    "file",
		"HASH_ALGORITHM": "md5",
		"PORT":           "8080",
		"ADMIN_TOKEN":    "link-admin-token",
	}
	cfg, err := loadFromEnv(func(k string) string { return env[k] })
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.LinksDir != DefaultLinksDir {
		t.Fatalf("LinksDir default: %q", cfg.LinksDir)
	}
}
