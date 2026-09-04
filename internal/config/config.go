package config

import (
	"fmt"
	"os"
	"path"
	"strconv"
	"strings"
)

// Config holds the runtime configuration of the file-sharing service.
//
// The container share root is fixed by the deployment contract at
// /opt/sharefiles. HOST_SHARE_PATH is a Compose-only interpolation variable
// and is intentionally not read here: it must never influence hashes or URLs.
type Config struct {
	// ContainerRoot is the fixed mount point inside the container.
	ContainerRoot string
	// SharePrefix is the SFTP-visible logical path prefix (e.g. "files").
	SharePrefix string
	// PublicURL is the external base URL used to render share links.
	PublicURL string
	// HashTarget is "file" (content hash) or "filename" (basename hash).
	HashTarget string
	// HashAlgorithm is "md5" or "sha256".
	HashAlgorithm string
	// Port is the HTTP listen port.
	Port string
	// AdminPath, when set, is the secret single-segment path (e.g.
	// "admin123456") where the file listing is served: only
	// GET /<AdminPath> renders it. Empty disables the listing
	// entirely and every index path returns 404.
	AdminPath string
	// AdminPassword, when set, additionally requires callers to submit
	// this password on the admin path before the listing is shown.
	// Empty means the secret path alone is sufficient.
	AdminPassword string
}

// DefaultContainerRoot is the fixed container mount point.
const DefaultContainerRoot = "/opt/sharefiles"

// NamespaceRoot returns the absolute directory the service scans and serves:
// <ContainerRoot>/<SharePrefix>.
func (c Config) NamespaceRoot() string {
	return path.Join(c.ContainerRoot, path.Clean("/"+c.SharePrefix))
}

// ShareURL joins the public URL with path segments without duplicate slashes.
func (c Config) ShareURL(dirHash, fileHash string) string {
	return strings.TrimRight(c.PublicURL, "/") + "/" + dirHash + "/" + fileHash
}

// Load reads configuration from the environment.
func Load() (Config, error) {
	return loadFromEnv(os.Getenv)
}

func loadFromEnv(getenv func(string) string) (Config, error) {
	cfg := Config{
		ContainerRoot: getenv("SHARE_ROOT"),
		SharePrefix:   getenv("SHARE_PREFIX"),
		PublicURL:     getenv("PUBLIC_URL"),
		HashTarget:    getenv("HASH_TARGET"),
		HashAlgorithm: getenv("HASH_ALGORITHM"),
		Port:          getenv("PORT"),
		AdminPath:     getenv("ADMIN_PATH"),
		AdminPassword: getenv("ADMIN_PASSWORD"),
	}
	if cfg.ContainerRoot == "" {
		cfg.ContainerRoot = DefaultContainerRoot
	}
	if cfg.HashTarget == "" {
		cfg.HashTarget = "file"
	}
	if cfg.HashAlgorithm == "" {
		cfg.HashAlgorithm = "md5"
	}
	if cfg.Port == "" {
		cfg.Port = "8080"
	}
	if cfg.PublicURL == "" {
		cfg.PublicURL = "http://localhost:" + cfg.Port
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Validate checks the configuration values.
func (c Config) Validate() error {
	if c.ContainerRoot == "" || !path.IsAbs(c.ContainerRoot) {
		return fmt.Errorf("invalid SHARE_ROOT %q: must be an absolute path", c.ContainerRoot)
	}
	if _, err := NormalizePrefix(c.SharePrefix); err != nil {
		return err
	}
	if c.HashTarget != "file" && c.HashTarget != "filename" {
		return fmt.Errorf("invalid HASH_TARGET %q: must be \"file\" or \"filename\"", c.HashTarget)
	}
	if c.HashAlgorithm != "md5" && c.HashAlgorithm != "sha256" {
		return fmt.Errorf("invalid HASH_ALGORITHM %q: must be \"md5\" or \"sha256\"", c.HashAlgorithm)
	}
	if c.PublicURL == "" {
		return fmt.Errorf("invalid PUBLIC_URL: must not be empty")
	}
	if _, err := strconv.Atoi(c.Port); err != nil {
		return fmt.Errorf("invalid PORT %q: must be numeric", c.Port)
	}
	if err := ValidateAdminPath(c.AdminPath); err != nil {
		return err
	}
	return nil
}

// ValidateAdminPath checks the secret listing path. Empty disables the
// listing and is always valid. A non-empty value must be a single URL path
// segment so it can never collide with /healthz or /{dirHash}/{fileHash}.
func ValidateAdminPath(p string) error {
	if p == "" {
		return nil
	}
	if len(p) < 8 {
		return fmt.Errorf("invalid ADMIN_PATH %q: must be at least 8 characters", p)
	}
	if len(p) > 128 {
		return fmt.Errorf("invalid ADMIN_PATH %q: must be at most 128 characters", p)
	}
	if strings.Contains(p, "/") || strings.Contains(p, "\\") {
		return fmt.Errorf("invalid ADMIN_PATH %q: must be a single path segment without slashes", p)
	}
	if p == "." || p == ".." || p == "healthz" {
		return fmt.Errorf("invalid ADMIN_PATH %q: reserved value", p)
	}
	for _, r := range p {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' ||
			r == '-' || r == '_' || r == '.' || r == '~' {
			continue
		}
		return fmt.Errorf("invalid ADMIN_PATH %q: only letters, digits, and -_.~ are allowed", p)
	}
	return nil
}

// NormalizePrefix cleans and validates the logical path prefix.
// The prefix must be given as a relative path: a leading slash is rejected
// so an absolute host/container path can never be mistaken for a prefix.
func NormalizePrefix(prefix string) (string, error) {
	p := strings.TrimSpace(prefix)
	if p == "" {
		return "", fmt.Errorf("invalid SHARE_PREFIX %q: must not be empty", prefix)
	}
	if strings.HasPrefix(p, "/") {
		return "", fmt.Errorf("invalid SHARE_PREFIX %q: must be a relative path", prefix)
	}
	p = strings.Trim(p, "/")
	if p == "" {
		return "", fmt.Errorf("invalid SHARE_PREFIX %q: must not be empty", prefix)
	}
	if strings.Contains(p, "\\") {
		return "", fmt.Errorf("invalid SHARE_PREFIX %q: must use forward slashes", prefix)
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return "", fmt.Errorf("invalid SHARE_PREFIX %q: must be a relative path without empty or dot segments", prefix)
		}
	}
	return path.Clean(p), nil
}
