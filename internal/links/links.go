// Package links implements scoped share-link storage for the file-sharing
// service: link CRUD over a mutex-guarded map persisted as JSON with
// atomic replacement, plus bcrypt password helpers.
package links

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	"simple-fileurl/internal/config"
)

// BcryptCost is the bcrypt work factor for link passwords.
const BcryptCost = 12

// ErrExists is returned when creating a link whose ID is already taken.
// ErrNotFound is returned when updating or deleting an unknown link.
var (
	ErrExists   = errors.New("link already exists")
	ErrNotFound = errors.New("link not found")
)

// ScopeType is "admin" (all namespaces) or "user" (files/ + one directory).
type ScopeType string

const (
	ScopeAdmin ScopeType = "admin"
	ScopeUser  ScopeType = "user"
)

// Scope is the visibility range of a link.
type Scope struct {
	Type ScopeType `json:"type"`
	User string    `json:"user,omitempty"`
}

// Validate checks the scope: admin takes no user, user requires a valid
// SFTP login name.
func (s Scope) Validate() error {
	switch s.Type {
	case ScopeAdmin:
		if s.User != "" {
			return fmt.Errorf("admin scope takes no user")
		}
		return nil
	case ScopeUser:
		if !config.ValidUsername(s.User) {
			return fmt.Errorf("invalid scope user %q", s.User)
		}
		return nil
	default:
		return fmt.Errorf("invalid scope type %q", s.Type)
	}
}

// Link is one shareable listing.
type Link struct {
	ID           string     `json:"id"`
	PasswordHash string     `json:"password_hash,omitempty"`
	Scope        Scope      `json:"scope"`
	CreatedAt    time.Time  `json:"created_at"`
	ExpiresAt    *time.Time `json:"expires_at,omitempty"`
	CreatedBy    string     `json:"created_by"`
	Description  string     `json:"description,omitempty"`
}

// HasPassword reports whether the link requires a password.
func (l Link) HasPassword() bool {
	return l.PasswordHash != ""
}

// Expired reports whether the link is past its expiry. A nil ExpiresAt
// never expires.
func (l Link) Expired(now time.Time) bool {
	return l.ExpiresAt != nil && !now.Before(*l.ExpiresAt)
}

// NewID generates a 16-character URL-safe random link ID.
func NewID() (string, error) {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}

// HashPassword bcrypt-hashes a link password.
func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), BcryptCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// CheckPassword compares a password against its bcrypt hash in constant
// time. An empty hash never matches.
func CheckPassword(hash, password string) bool {
	if hash == "" {
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// Store is a mutex-guarded link map backed by links.json.
type Store struct {
	mu    sync.Mutex
	path  string
	links map[string]Link
}

// NewStore creates a Store persisting to dir/links.json.
func NewStore(dir string) *Store {
	return &Store{path: filepath.Join(dir, "links.json"), links: map[string]Link{}}
}

// Load reads links.json. A missing file means an empty store.
func (s *Store) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			s.links = map[string]Link{}
			return nil
		}
		return err
	}
	var doc struct {
		Links map[string]Link `json:"links"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return err
	}
	if doc.Links == nil {
		doc.Links = map[string]Link{}
	}
	s.links = doc.Links
	return nil
}

// saveLocked writes the store atomically and purges expired entries.
func (s *Store) saveLocked() error {
	for id, l := range s.links {
		if l.Expired(time.Now()) {
			delete(s.links, id)
		}
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	raw, err := json.Marshal(struct {
		Links map[string]Link `json:"links"`
	}{Links: s.links})
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), "links-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
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
	return os.Rename(tmpName, s.path)
}

// Create stores a new link. Duplicate IDs are rejected.
func (s *Store) Create(l Link) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.links[l.ID]; ok {
		return ErrExists
	}
	s.links[l.ID] = l
	return s.saveLocked()
}

// Get returns a copy of the link. Expired links are still returned;
// callers enforce expiry.
func (s *Store) Get(id string) (Link, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	l, ok := s.links[id]
	return l, ok
}

// Update mutates a link atomically and persists the result.
func (s *Store) Update(id string, mutate func(*Link) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	l, ok := s.links[id]
	if !ok {
		return ErrNotFound
	}
	if err := mutate(&l); err != nil {
		return err
	}
	s.links[id] = l
	return s.saveLocked()
}

// Delete removes a link. Unknown IDs are not an error.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.links, id)
	return s.saveLocked()
}

// List returns all links ordered by creation time, then ID.
func (s *Store) List() []Link {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Link, 0, len(s.links))
	for _, l := range s.links {
		out = append(out, l)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out
}
