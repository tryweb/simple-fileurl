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
	"strings"
)

// DefaultUsersFile is the only manifest path the admin reads and writes.
// It lives on the sftp-users named volume shared with the SFTP container.
const DefaultUsersFile = "/var/lib/sftp-users/users.json"

// DefaultAddr is the container listen address of the admin UI.
const DefaultAddr = ":8080"

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
	}
	if cfg.Password == "" {
		return Config{}, errors.New("sftpadmin: SFTP_ADMIN_PASSWORD is required")
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
