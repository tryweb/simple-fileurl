package config

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
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
	// AdminToken is the Bearer token for the share-link management API.
	// Required: a missing value fails fast at startup.
	AdminToken string
	// LinksDir is the directory holding links.json, on a named volume.
	LinksDir string
	// WebGID is the group ID for the web readers group. Per-user
	// directories are created with this group so the web service can
	// read files while SFTP users cannot access other users' directories.
	WebGID string
}

// DefaultContainerRoot is the fixed container mount point.
const DefaultContainerRoot = "/opt/sharefiles"

// DefaultWebGID is the default group ID for the web readers group.
const DefaultWebGID = "2001"

// DefaultLinksDir is the default directory holding links.json.
const DefaultLinksDir = "/var/lib/file-links"

// usernamePattern is the deployment contract for SFTP login names, shared
// with the SFTP reconciler and admin validation: lowercase start, max 32
// chars. Only top-level directories matching it (or the shared prefix)
// form servable namespaces.
var usernamePattern = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)

// NamespaceRoot returns the container share root (e.g. /opt/sharefiles).
// Every eligible top-level directory beneath it — the shared prefix and
// each per-user directory — forms an independent logical namespace.
func (c Config) NamespaceRoot() string {
	return path.Clean(c.ContainerRoot)
}

// ValidUsername reports whether name is a valid SFTP login name, shared
// with link scope validation: lowercase start, max 32 chars.
func ValidUsername(name string) bool {
	return usernamePattern.MatchString(name)
}

// IsEligibleNamespace reports whether a top-level directory name under the
// container share root is servable: the shared prefix or a valid SFTP
// username. Anything else is ignored by the scan and never served.
func (c Config) IsEligibleNamespace(name string) bool {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\`) {
		return false
	}
	if prefix, err := NormalizePrefix(c.SharePrefix); err == nil {
		if name == strings.Split(prefix, "/")[0] {
			return true
		}
	}
	return usernamePattern.MatchString(name)
}

// CheckFilesystem verifies the container share root exists and contains the
// shared prefix directory. Per-user directories are optional: startup
// succeeds with only the shared prefix present, and fails when the prefix
// directory itself is missing.
func (c Config) CheckFilesystem() error {
	root := filepath.FromSlash(path.Clean(c.ContainerRoot))
	st, err := os.Stat(root)
	if err != nil {
		return fmt.Errorf("share root %q: %w", root, err)
	}
	if !st.IsDir() {
		return fmt.Errorf("share root %q is not a directory", root)
	}
	prefix, err := NormalizePrefix(c.SharePrefix)
	if err != nil {
		return err
	}
	top := strings.Split(prefix, "/")[0]
	pst, err := os.Stat(filepath.Join(root, top))
	if err != nil {
		return fmt.Errorf("share prefix dir %q: %w", top, err)
	}
	if !pst.IsDir() {
		return fmt.Errorf("share prefix dir %q is not a directory", top)
	}
	return nil
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
		AdminToken:    getenv("ADMIN_TOKEN"),
		LinksDir:      getenv("LINKS_DIR"),
		WebGID:        getenv("WEB_GID"),
	}
	if cfg.ContainerRoot == "" {
		cfg.ContainerRoot = DefaultContainerRoot
	}
	if cfg.WebGID == "" {
		cfg.WebGID = DefaultWebGID
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
	if cfg.LinksDir == "" {
		cfg.LinksDir = DefaultLinksDir
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
	if c.AdminToken == "" {
		return fmt.Errorf("invalid ADMIN_TOKEN: must not be empty")
	}
	if c.LinksDir == "" || !path.IsAbs(c.LinksDir) {
		return fmt.Errorf("invalid LINKS_DIR %q: must be an absolute path", c.LinksDir)
	}
	if _, err := strconv.Atoi(c.WebGID); err != nil {
		return fmt.Errorf("invalid WEB_GID %q: must be a numeric GID", c.WebGID)
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
