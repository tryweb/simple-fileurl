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

// Store scans the configured namespace and resolves hash URLs.
type Store struct {
	cfg   config.Config
	mu    sync.Mutex
	cache map[string]fileCache
}

// New creates a Store bound to cfg.
func New(cfg config.Config) *Store {
	return &Store{cfg: cfg, cache: map[string]fileCache{}}
}

// NamespaceRoot returns the absolute scan root.
func (s *Store) NamespaceRoot() string {
	return filepath.FromSlash(s.cfg.NamespaceRoot())
}

// Check verifies the namespace exists and is a usable directory.
func (s *Store) Check() error {
	st, err := os.Stat(s.NamespaceRoot())
	if err != nil {
		return err
	}
	if !st.IsDir() {
		return errors.New("namespace root is not a directory")
	}
	return nil
}

// List scans the namespace and returns shareable entries sorted by logical path.
func (s *Store) List() ([]Entry, error) {
	return s.scan()
}

// Resolve maps (dirHash, fileHash) to a file on disk.
func (s *Store) Resolve(dirHash, fileHash string) (string, Entry, error) {
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
	root := s.NamespaceRoot()
	prefix, err := config.NormalizePrefix(s.cfg.SharePrefix)
	if err != nil {
		return nil, err
	}
	var entries []Entry
	walkErr := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
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
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return nil
		}
		relSlash := filepath.ToSlash(rel)
		var logicalDir string
		dir := path.Dir(relSlash)
		if dir == "." {
			logicalDir = prefix
		} else {
			logicalDir = prefix + "/" + dir
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
		entries = append(entries, Entry{
			LogicalPath: logicalPath,
			LogicalDir:  logicalDir,
			DirHash:     dirHash,
			FileHash:    fileHash,
			Size:        info.Size(),
		})
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].LogicalPath < entries[j].LogicalPath })
	return entries, nil
}

func (s *Store) contentHash(abs string, info fs.FileInfo) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c, ok := s.cache[abs]; ok && c.size == info.Size() && c.mtime == info.ModTime().UnixNano() {
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
	s.cache[abs] = fileCache{size: info.Size(), mtime: info.ModTime().UnixNano(), hash: h}
	return h, nil
}

// openPath re-validates the resolved file: regular, non-symlink, inside the
// namespace. It returns the absolute path for streaming.
func (s *Store) openPath(e Entry) (string, error) {
	root := s.NamespaceRoot()
	// Rebuild the absolute path from the logical path components only.
	cleanRel := path.Clean(strings.TrimPrefix(e.LogicalPath, mustPrefix(s.cfg.SharePrefix)))
	cleanRel = strings.TrimPrefix(cleanRel, "/")
	abs := filepath.Join(root, filepath.FromSlash(cleanRel))
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

func mustPrefix(p string) string {
	n, err := config.NormalizePrefix(p)
	if err != nil {
		return strings.Trim(p, "/")
	}
	return n
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
