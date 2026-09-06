package store

import (
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"simple-fileurl/internal/config"
	"simple-fileurl/internal/hash"
)

// Sentinel resolution errors. HTTP mapping: NotFound -> 404,
// Conflict -> 409, Invalid -> 400.
var (
	ErrNotFound = errors.New("no file matches the requested hashes")
	ErrConflict = errors.New("hash collision: multiple files match")
	ErrInvalid  = errors.New("invalid hash format")
)

// Entry describes one shareable regular file.
type Entry struct {
	// LogicalPath is the SFTP-visible path, e.g. "files/mydir/cron.txt".
	LogicalPath string
	// LogicalDir is the SFTP-visible directory, e.g. "files/mydir".
	LogicalDir string
	// DirHash is the hash of LogicalDir.
	DirHash string
	// FileHash is the content or filename hash.
	FileHash string
	// Size is the file size in bytes.
	Size int64
}

type fileCache struct {
	size  int64
	mtime int64
	hash  string
}

// Store scans the configured namespace and resolves hash URLs. The live
// configuration and the content-hash cache are guarded by mu: ListWith and
// ResolveWith swap the whole config atomically, so concurrent readers never
// observe a torn mix of two configurations or stale hashes.
type Store struct {
	cfg   config.Config
	mu    sync.RWMutex
	cache map[string]fileCache
}

// New creates a Store bound to cfg.
func New(cfg config.Config) *Store {
	return &Store{cfg: cfg, cache: map[string]fileCache{}}
}

// NamespaceRoot returns the absolute scan root. It takes the read lock so
// concurrent live-config swaps in ListWith/ResolveWith cannot race it.
func (s *Store) NamespaceRoot() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.namespaceRootLocked()
}

// namespaceRootLocked reports the scan root. Callers must hold mu.
func (s *Store) namespaceRootLocked() string {
	return filepath.FromSlash(s.cfg.NamespaceRoot())
}

// Check verifies the container share root exists and holds the shared
// prefix directory. Per-user directories are optional.
func (s *Store) Check() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg.CheckFilesystem()
}

// List scans the namespace and returns shareable entries sorted by logical path.
func (s *Store) List() ([]Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.scan()
}

// setConfigLocked swaps the live configuration and drops the content-hash
// cache whenever anything changed. Cache keys already carry the algorithm
// and target, but clearing on swap bounds memory and guarantees a new
// configuration never observes hashes computed under an older one.
// Callers must hold the write lock.
func (s *Store) setConfigLocked(cfg config.Config) {
	if s.cfg != cfg {
		s.cfg = cfg
		s.cache = map[string]fileCache{}
	}
}

// ListWith swaps in cfg for live config updates, then scans atomically:
// one listing never mixes two configurations.
func (s *Store) ListWith(cfg config.Config) ([]Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.setConfigLocked(cfg)
	return s.scan()
}

// Resolve maps (dirHash, fileHash) to a file on disk.
func (s *Store) Resolve(dirHash, fileHash string) (string, Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.resolveLocked(dirHash, fileHash)
}

// ResolveWith swaps in cfg for live config updates, then resolves
// atomically, so listing and download always agree on one configuration.
func (s *Store) ResolveWith(cfg config.Config, dirHash, fileHash string) (string, Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.setConfigLocked(cfg)
	return s.resolveLocked(dirHash, fileHash)
}

func (s *Store) resolveLocked(dirHash, fileHash string) (string, Entry, error) {
	if !hash.ValidHex(dirHash, s.cfg.HashAlgorithm) || !hash.ValidHex(fileHash, s.cfg.HashAlgorithm) {
		return "", Entry{}, ErrInvalid
	}
	entries, err := s.scan()
	if err != nil {
		return "", Entry{}, err
	}
	var matches []Entry
	for _, e := range entries {
		if e.DirHash == dirHash && e.FileHash == fileHash {
			matches = append(matches, e)
		}
	}
	if len(matches) == 0 {
		return "", Entry{}, ErrNotFound
	}
	if len(matches) > 1 {
		return "", Entry{}, ErrConflict
	}
	abs, err := s.openPath(matches[0])
	if err != nil {
		return "", Entry{}, err
	}
	return abs, matches[0], nil
}

func (s *Store) scan() ([]Entry, error) {
	root := s.namespaceRootLocked()
	top, err := os.ReadDir(root)
	if err != nil {
		// A missing or unreadable root serves nothing; Check reports the
		// unhealthy state separately at startup and on /healthz.
		if os.IsNotExist(err) || os.IsPermission(err) {
			return nil, nil
		}
		return nil, err
	}
	var entries []Entry
	for _, d := range top {
		// Only eligible top-level directories form namespaces: the shared
		// prefix and valid SFTP usernames. Anything else is ignored, and
		// files directly under the root belong to no namespace.
		if !d.IsDir() || !s.cfg.IsEligibleNamespace(d.Name()) {
			continue
		}
		if err := s.scanNamespace(filepath.Join(root, d.Name()), d.Name(), &entries); err != nil {
			return nil, err
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].LogicalPath < entries[j].LogicalPath })
	return entries, nil
}

// scanNamespace walks one eligible top-level directory, building logical
// paths with the directory name as prefix (e.g. "files/mydir/cron.txt" or
// "jonathan/docs/notes.pdf").
func (s *Store) scanNamespace(nsRoot, namespace string, entries *[]Entry) error {
	return filepath.WalkDir(nsRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsPermission(err) || os.IsNotExist(err) {
				return nil
			}
			return err
		}
		// Never follow symlinks: skip them entirely. WalkDir does not
		// descend into symlinked directories, so no SkipDir is needed.
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(nsRoot, p)
		if err != nil {
			return nil
		}
		relSlash := filepath.ToSlash(rel)
		var logicalDir string
		dir := path.Dir(relSlash)
		if dir == "." {
			logicalDir = namespace
		} else {
			logicalDir = namespace + "/" + dir
		}
		logicalPath := logicalDir + "/" + d.Name()
		dirHash, err := hash.Directory(logicalDir, s.cfg.HashAlgorithm)
		if err != nil {
			return nil
		}
		var fileHash string
		if s.cfg.HashTarget == "filename" {
			fileHash, err = hash.Filename(d.Name(), s.cfg.HashAlgorithm)
			if err != nil {
				return nil
			}
		} else {
			fileHash, err = s.contentHash(p, info)
			if err != nil {
				// Unreadable files are omitted from the listing instead of
				// producing links the download endpoint cannot resolve.
				if os.IsPermission(err) || os.IsNotExist(err) {
					return nil
				}
				return err
			}
		}
		*entries = append(*entries, Entry{
			LogicalPath: logicalPath,
			LogicalDir:  logicalDir,
			DirHash:     dirHash,
			FileHash:    fileHash,
			Size:        info.Size(),
		})
		return nil
	})
}

// contentHash hashes file contents with a size+mtime cache. Callers must
// hold mu: the cache key includes the algorithm and target so a live
// config change never serves hashes computed under another configuration.
func (s *Store) contentHash(abs string, info fs.FileInfo) (string, error) {
	key := abs + "\x00" + s.cfg.HashAlgorithm + "\x00" + s.cfg.HashTarget
	if c, ok := s.cache[key]; ok && c.size == info.Size() && c.mtime == info.ModTime().UnixNano() {
		return c.hash, nil
	}
	f, err := os.Open(abs)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h, err := hash.Reader(f, s.cfg.HashAlgorithm)
	if err != nil {
		return "", err
	}
	s.cache[key] = fileCache{size: info.Size(), mtime: info.ModTime().UnixNano(), hash: h}
	return h, nil
}

// openPath re-validates the resolved file: regular, non-symlink, inside an
// eligible namespace. It returns the absolute path for streaming.
func (s *Store) openPath(e Entry) (string, error) {
	root := s.namespaceRootLocked()
	// The logical path's first segment selects the namespace; only files
	// in eligible namespaces (shared prefix or valid SFTP usernames)
	// resolve.
	clean := strings.TrimPrefix(path.Clean("/"+e.LogicalPath), "/")
	if clean == "" || clean == "." || clean == ".." {
		return "", ErrNotFound
	}
	top := clean
	if i := strings.Index(clean, "/"); i >= 0 {
		top = clean[:i]
	}
	if !s.cfg.IsEligibleNamespace(top) {
		return "", ErrNotFound
	}
	abs := filepath.Join(root, filepath.FromSlash(clean))
	if !withinRoot(root, abs) {
		return "", ErrNotFound
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return "", ErrNotFound
		}
		return "", err
	}
	if !withinRoot(root, resolved) {
		return "", ErrNotFound
	}
	// The resolved target must stay inside an eligible namespace, so a
	// symlink cannot escape into an ineligible top-level directory.
	rel, err := filepath.Rel(root, resolved)
	if err != nil {
		return "", ErrNotFound
	}
	relSlash := filepath.ToSlash(rel)
	seg := relSlash
	if i := strings.Index(relSlash, "/"); i >= 0 {
		seg = relSlash[:i]
	}
	if !s.cfg.IsEligibleNamespace(seg) {
		return "", ErrNotFound
	}
	st, err := os.Lstat(resolved)
	if err != nil {
		if os.IsNotExist(err) {
			return "", ErrNotFound
		}
		return "", err
	}
	if st.Mode()&fs.ModeSymlink != 0 || !st.Mode().IsRegular() {
		return "", ErrNotFound
	}
	return resolved, nil
}

func withinRoot(root, abs string) bool {
	rootClean := filepath.Clean(root)
	absClean := filepath.Clean(abs)
	if absClean == rootClean {
		return false
	}
	rel, err := filepath.Rel(rootClean, absClean)
	if err != nil {
		return false
	}
	return rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
