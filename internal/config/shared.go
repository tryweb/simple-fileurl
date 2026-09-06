// Package config shared runtime settings: the JSON config file that the
// admin UI writes and both services read for live updates without restart.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// DefaultSharedConfigPath is the config file location inside containers.
const DefaultSharedConfigPath = "/etc/app/config.json"

// SharedConfig holds the runtime-editable settings shared by file-sharing
// and sftp-admin. It mirrors a subset of Config plus the sftp-admin
// password; deployment constants (ports, GIDs, mount paths) stay in env.
type SharedConfig struct {
	HashAlgorithm     string `json:"hash_algorithm"`
	HashTarget        string `json:"hash_target"`
	PublicURL         string `json:"public_url"`
	AdminToken        string `json:"admin_token"`
	SftpAdminPassword string `json:"sftp_admin_password"`
}

// Validate checks shared values with the same rules as startup config.
func (c SharedConfig) Validate() error {
	if c.HashTarget != "file" && c.HashTarget != "filename" {
		return fmt.Errorf("invalid HASH_TARGET %q: must be \"file\" or \"filename\"", c.HashTarget)
	}
	if c.HashAlgorithm != "md5" && c.HashAlgorithm != "sha256" {
		return fmt.Errorf("invalid HASH_ALGORITHM %q: must be \"md5\" or \"sha256\"", c.HashAlgorithm)
	}
	if c.PublicURL == "" {
		return fmt.Errorf("invalid PUBLIC_URL: must not be empty")
	}
	if c.AdminToken == "" {
		return fmt.Errorf("invalid ADMIN_TOKEN: must not be empty")
	}
	if c.SftpAdminPassword == "" {
		return fmt.Errorf("invalid SFTP_ADMIN_PASSWORD: must not be empty")
	}
	return nil
}

// LoadShared reads and validates the shared config file. A missing or
// invalid file is an error; callers fall back to startup config.
func LoadShared(path string) (SharedConfig, error) {
	var c SharedConfig
	raw, err := os.ReadFile(path)
	if err != nil {
		return SharedConfig{}, err
	}
	if err := json.Unmarshal(raw, &c); err != nil {
		return SharedConfig{}, err
	}
	if err := c.Validate(); err != nil {
		return SharedConfig{}, err
	}
	return c, nil
}

// Save writes the config atomically after validation. The temporary file
// stays owner-only while secrets are written; the published file is 0640
// with the config directory's group, so the non-root file-sharing reader
// (supplementary webreaders group, setgid /etc/app in the images) keeps
// read access without any world permission. Writers must mount /etc/app
// read-write (sftp-admin); readers mount it read-only (file-sharing).
func (c SharedConfig) Save(path string) error {
	if err := c.Validate(); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "config-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	// Best effort: inherit the config directory's group so a group reader
	// survives even on volumes created before the setgid bit existed.
	// Failures are ignored; the rename below still publishes the file.
	if st, err := os.Stat(filepath.Dir(path)); err == nil {
		if sys, ok := st.Sys().(*syscall.Stat_t); ok {
			_ = os.Chown(tmpName, -1, int(sys.Gid))
		}
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	return os.Chmod(path, 0o640)
}
