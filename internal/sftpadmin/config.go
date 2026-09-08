// Package sftpadmin implements the standalone SFTP admin UI.
//
// The admin touches exactly one host path: the shared user manifest at
// /var/lib/sftp-users/users.json (see DefaultUsersFile). It never mounts or
// reads the host share data or SFTP server files: user reconciliation is
// the SFTP container's job. Manifest writes use a temporary file plus fsync
// and an atomic rename, so readers only ever see complete documents.
package sftpadmin

import (
	"errors"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"simple-fileurl/internal/config"
)

// DefaultUsersFile is the only manifest path the admin reads and writes.
// It lives on the sftp-users named volume shared with the SFTP container.
const DefaultUsersFile = "/var/lib/sftp-users/users.json"

// DefaultAddr is the container listen address of the admin UI.
const DefaultAddr = ":8080"

// DefaultLinksDir is the directory for the links store.
const DefaultLinksDir = "/var/lib/file-links"

// Config holds the runtime configuration of the admin UI.
type Config struct {
	// Password is the single admin password. Required.
	Password string
	// Addr is the listen address, e.g. ":8080".
	Addr string
	// UsersFile is the manifest path. Defaults to DefaultUsersFile.
	UsersFile string
	// SeedUser and SeedPubKey optionally create the first user, but only
	// when the manifest file does not exist yet. Both must be set together.
	SeedUser   string
	SeedPubKey string
	// SharedPath is the shared config.json location read for live
	// settings. Defaults to config.DefaultSharedConfigPath.
	SharedPath string
	// LinksDir is the directory containing links.json for share link
	// management. Defaults to DefaultLinksDir.
	LinksDir string
}

// LoadConfig reads configuration from the environment. A missing
// SFTP_ADMIN_PASSWORD is a hard error so the container fails fast.
func LoadConfig(getenv func(string) string) (Config, error) {
	if getenv == nil {
		getenv = os.Getenv
	}
	cfg := Config{
		Password:   getenv("SFTP_ADMIN_PASSWORD"),
		Addr:       getenv("PORT"),
		UsersFile:  getenv("SFTP_USERS_FILE"),
		SeedUser:   getenv("SFTP_SEED_USER"),
		SeedPubKey: getenv("SFTP_SEED_PUBKEY"),
		SharedPath: getenv("SHARED_CONFIG_PATH"),
		LinksDir:   getenv("LINKS_DIR"),
	}
	if cfg.SharedPath == "" {
		cfg.SharedPath = config.DefaultSharedConfigPath
	}
	if !path.IsAbs(cfg.SharedPath) {
		return Config{}, errors.New("sftpadmin: SHARED_CONFIG_PATH must be an absolute path")
	}
	if cfg.LinksDir == "" {
		cfg.LinksDir = DefaultLinksDir
	}
	// The password may come from the environment or the shared config
	// file; with neither source the container fails fast.
	if cfg.Password == "" {
		if sc, err := config.LoadShared(cfg.SharedPath); err != nil || sc.SftpAdminPassword == "" {
			return Config{}, errors.New("sftpadmin: SFTP_ADMIN_PASSWORD is required")
		}
	}
	if cfg.Addr == "" {
		cfg.Addr = DefaultAddr
	} else if !strings.Contains(cfg.Addr, ":") {
		cfg.Addr = ":" + cfg.Addr
	}
	if cfg.UsersFile == "" {
		cfg.UsersFile = DefaultUsersFile
	}
	if (cfg.SeedUser == "") != (cfg.SeedPubKey == "") {
		return Config{}, errors.New("sftpadmin: SFTP_SEED_USER and SFTP_SEED_PUBKEY must be set together")
	}
	if cfg.SeedUser != "" {
		if err := ValidateUsername(cfg.SeedUser); err != nil {
			return Config{}, err
		}
		if _, _, err := ValidatePublicKey(cfg.SeedPubKey); err != nil {
			return Config{}, err
		}
	}
	return cfg, nil
}

// SeedSharedConfig creates the shared config file from environment values
// on first boot. An existing file is never touched. Hash settings fall
// back to the file-sharing defaults; the two secrets are required, so a
// missing token or password fails fast instead of seeding a broken file.
func SeedSharedConfig(path string, getenv func(string) string) error {
	if getenv == nil {
		getenv = os.Getenv
	}
	if err := prepareSharedPermissions(path, getenv("WEB_GID")); err != nil {
		return err
	}
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	port := getenv("PORT")
	if port == "" {
		port = "8080"
	}
	publicURL := getenv("PUBLIC_URL")
	if publicURL == "" {
		publicURL = "http://localhost:" + port
	}
	algo := getenv("HASH_ALGORITHM")
	if algo == "" {
		algo = "md5"
	}
	target := getenv("HASH_TARGET")
	if target == "" {
		target = "file"
	}
	return config.SharedConfig{
		HashAlgorithm:     algo,
		HashTarget:        target,
		PublicURL:         publicURL,
		AdminToken:        getenv("ADMIN_TOKEN"),
		SftpAdminPassword: getenv("SFTP_ADMIN_PASSWORD"),
	}.Save(path)
}

func prepareSharedPermissions(filePath, gidText string) error {
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0o770); err != nil {
		return err
	}
	if gidText != "" {
		gid, err := strconv.Atoi(gidText)
		if err != nil || gid < 0 {
			return errors.New("sftpadmin: WEB_GID must be a non-negative integer")
		}
		if err := os.Chown(dir, -1, gid); err != nil {
			return err
		}
	}
	if err := os.Chmod(dir, 0o2770); err != nil {
		return err
	}
	if _, err := os.Stat(filePath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if gidText != "" {
		gid, _ := strconv.Atoi(gidText)
		if err := os.Chown(filePath, -1, gid); err != nil {
			return err
		}
	}
	return os.Chmod(filePath, 0o640)
}
