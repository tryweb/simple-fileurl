package sftpadmin

import (
	"encoding/json"
	"errors"
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

// ErrManifestInvalid marks a strict final-manifest validation failure:
// the mutation was refused and the file left untouched. Handlers map it
// to 409 so operators know dedicated repair is required.
var ErrManifestInvalid = errors.New("invalid manifest")

// Update loads the manifest, applies fn, revalidates, and persists the
// result atomically. fn's error aborts without touching the file. The final
// document is strictly validated, so writes never introduce invalid keys
// even though reads tolerate legacy ones.
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
	if err := validateManifest(m, true); err != nil {
		return fmt.Errorf("%w: %w", ErrManifestInvalid, err)
	}
	return s.writeLocked(m)
}

// MaxKeyNoteLength bounds per-key operator notes. Notes are display-only
// sidecar data: they never affect validation, reconciliation, or
// fingerprints, and their contents are never logged.
const MaxKeyNoteLength = 120

// keyNoteID maps a note to its key. Fingerprints are canonical and unique
// per key material, so username + "|" + fingerprint is a stable sidecar key.
func keyNoteID(username, fp string) string {
	return username + "|" + fp
}

// notesPath is the sidecar file next to the manifest.
func (s *Store) notesPath() string {
	return filepath.Join(filepath.Dir(s.path), "key-notes.json")
}

// LoadNotes reads the sidecar map. A missing file yields an empty map so
// first boot works; a corrupt file is an error the dashboard treats as
// empty rather than failing the page.
func (s *Store) LoadNotes() (map[string]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadNotesLocked()
}

func (s *Store) loadNotesLocked() (map[string]string, error) {
	data, err := os.ReadFile(s.notesPath())
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]string{}, nil
		}
		return nil, err
	}
	notes := map[string]string{}
	if err := json.Unmarshal(data, &notes); err != nil {
		return nil, fmt.Errorf("invalid key notes: %w", err)
	}
	return notes, nil
}

// SetNote stores (or, when note is empty, clears) the operator note for one
// key. Callers must have resolved the user and fingerprint already.
func (s *Store) SetNote(username, fp, note string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	notes, err := s.loadNotesLocked()
	if err != nil {
		return err
	}
	if note == "" {
		delete(notes, keyNoteID(username, fp))
	} else {
		notes[keyNoteID(username, fp)] = note
	}
	return s.writeNotesLocked(notes)
}

// DeleteNote drops the sidecar entry for a removed key. Missing entries are
// a no-op that leaves the file untouched.
func (s *Store) DeleteNote(username, fp string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	notes, err := s.loadNotesLocked()
	if err != nil {
		return err
	}
	id := keyNoteID(username, fp)
	if _, ok := notes[id]; !ok {
		return nil
	}
	delete(notes, id)
	return s.writeNotesLocked(notes)
}

// PruneNotes drops every sidecar entry not in keep (the set of live key
// IDs). Repair and key removal call this so orphan notes never accumulate
// while notes on surviving keys are preserved.
func (s *Store) PruneNotes(keep map[string]bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	notes, err := s.loadNotesLocked()
	if err != nil {
		return err
	}
	changed := false
	for id := range notes {
		if !keep[id] {
			delete(notes, id)
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return s.writeNotesLocked(notes)
}

func (s *Store) writeNotesLocked(notes map[string]string) error {
	data, err := json.MarshalIndent(notes, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".key-notes-*.tmp")
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
	return os.Rename(tmpName, s.notesPath())
}

// RepairReport summarizes one manifest-wide invalid-key repair.
type RepairReport struct {
	// RemovedKeys counts dropped invalid entries across all users.
	RemovedKeys int
	// DisabledUsers names users left with zero valid keys.
	DisabledUsers []string
}

// RemoveInvalidKeys drops every authorized-key entry that fails canonical
// validation, preserving all valid keys and unrelated user fields. Users
// left with zero valid keys are disabled with a valid empty key set so no
// stale authorization stays effective. The whole repair holds the store
// mutex and persists atomically; with nothing invalid it is a no-op that
// leaves the file untouched.
func (s *Store) RemoveInvalidKeys() (RepairReport, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.loadLocked()
	if err != nil {
		return RepairReport{}, err
	}
	var rep RepairReport
	for i := range m.Users {
		orig := m.Users[i].AuthorizedKeys
		kept := make([]string, 0, len(orig))
		for _, k := range orig {
			if _, _, err := ValidatePublicKey(k); err != nil {
				rep.RemovedKeys++
				continue
			}
			kept = append(kept, k)
		}
		if len(kept) == len(orig) {
			continue
		}
		m.Users[i].AuthorizedKeys = kept
		if len(orig) > 0 && len(kept) == 0 {
			m.Users[i].Enabled = false
			rep.DisabledUsers = append(rep.DisabledUsers, m.Users[i].Username)
		}
	}
	if rep.RemovedKeys == 0 {
		return rep, nil
	}
	m.Version = ManifestVersion
	if m.Users == nil {
		m.Users = []User{}
	}
	if err := validateManifest(m, true); err != nil {
		return RepairReport{}, fmt.Errorf("%w: %w", ErrManifestInvalid, err)
	}
	return rep, s.writeLocked(m)
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
	// Reads tolerate legacy invalid authorized keys so one bad entry
	// cannot take down the users page; rendering marks them "(invalid)".
	if err := validateManifest(m, false); err != nil {
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
// unique usernames, and (strict only) well-formed public keys. Reads pass
// strict=false so legacy invalid keys load and render as "(invalid)";
// writes pass strict=true so no new invalid key is ever persisted.
func validateManifest(m Manifest, strict bool) error {
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
		if u.AuthorizedKeys == nil {
			u.AuthorizedKeys = []string{}
		}
		if !strict {
			continue
		}
		for _, k := range u.AuthorizedKeys {
			if _, _, err := ValidatePublicKey(k); err != nil {
				return fmt.Errorf("invalid manifest: user %q: %w", u.Username, err)
			}
		}
	}
	return nil
}
