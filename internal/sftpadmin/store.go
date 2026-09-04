package sftpadmin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// ManifestVersion is the only schema version the admin and the SFTP
// reconciler agree on (see design.md: users.json).
const ManifestVersion = 1

// User is one SFTP login: name, on/off switch, and public keys only.
// There are no per-user passwords: authentication is public-key-only.
type User struct {
	Username       string   `json:"username"`
	Enabled        bool     `json:"enabled"`
	AuthorizedKeys []string `json:"authorized_keys"`
}

// Manifest is the whole cross-container control plane document.
type Manifest struct {
	Version int    `json:"version"`
	Users   []User `json:"users"`
}

// Store guards the manifest file with a process-level mutex and persists
// every mutation via temporary file + fsync + atomic rename, so concurrent
// readers (including the SFTP reconciler in another container) only ever
// observe complete documents.
type Store struct {
	mu   sync.Mutex
	path string
}

// NewStore binds a manifest path. Only this path is ever read or written.
func NewStore(path string) *Store {
	return &Store{path: path}
}

// Path reports the single manifest file this store touches.
func (s *Store) Path() string {
	return s.path
}

// Load reads and validates the manifest. A missing file yields an empty
// version-1 manifest so first boot with seed variables works.
func (s *Store) Load() (Manifest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadLocked()
}

// Update loads the manifest, applies fn, revalidates, and persists the
// result atomically. fn's error aborts without touching the file.
func (s *Store) Update(fn func(*Manifest) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.loadLocked()
	if err != nil {
		return err
	}
	if err := fn(&m); err != nil {
		return err
	}
	m.Version = ManifestVersion
	if m.Users == nil {
		m.Users = []User{}
	}
	if err := validateManifest(m); err != nil {
		return err
	}
	return s.writeLocked(m)
}

func (s *Store) loadLocked() (Manifest, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return Manifest{Version: ManifestVersion, Users: []User{}}, nil
		}
		return Manifest{}, err
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return Manifest{}, fmt.Errorf("invalid manifest: %w", err)
	}
	if err := validateManifest(m); err != nil {
		return Manifest{}, err
	}
	return m, nil
}

// writeLocked replaces the manifest atomically: new content goes to a
// temporary sibling file, is fsynced to stable storage, chmodded 0600, and
// then renamed over the target in one atomic step.
func (s *Store) writeLocked(m Manifest) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".users-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
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
	if err := os.Chmod(tmpName, 0o600); err != nil {
		return err
	}
	return os.Rename(tmpName, s.path)
}

// validateManifest enforces the deployment contract: version 1, valid and
// unique usernames, and well-formed public keys only.
func validateManifest(m Manifest) error {
	if m.Version != ManifestVersion {
		return fmt.Errorf("invalid manifest: unsupported version %d", m.Version)
	}
	seen := make(map[string]bool, len(m.Users))
	for i := range m.Users {
		u := &m.Users[i]
		if err := ValidateUsername(u.Username); err != nil {
			return err
		}
		if seen[u.Username] {
			return fmt.Errorf("invalid manifest: duplicate user %q", u.Username)
		}
		seen[u.Username] = true
		for _, k := range u.AuthorizedKeys {
			if _, _, err := ValidatePublicKey(k); err != nil {
				return fmt.Errorf("invalid manifest: user %q: %w", u.Username, err)
			}
		}
		if u.AuthorizedKeys == nil {
			u.AuthorizedKeys = []string{}
		}
	}
	return nil
}
