package hash

import (
	"crypto/md5"
	"crypto/sha256"
	"fmt"
	"hash"
	"io"
	"strings"
)

// Algorithms supported by the service.
const (
	MD5    = "md5"
	SHA256 = "sha256"
)

// New returns a hash.Hash for the named algorithm.
func New(algorithm string) (hash.Hash, error) {
	switch algorithm {
	case MD5:
		return md5.New(), nil
	case SHA256:
		return sha256.New(), nil
	default:
		return nil, fmt.Errorf("unsupported hash algorithm %q", algorithm)
	}
}

// HexLength returns the lowercase hex digest length for the algorithm.
func HexLength(algorithm string) (int, error) {
	switch algorithm {
	case MD5:
		return 32, nil
	case SHA256:
		return 64, nil
	default:
		return 0, fmt.Errorf("unsupported hash algorithm %q", algorithm)
	}
}

// ValidHex reports whether s is a lowercase hex digest for the algorithm.
func ValidHex(s, algorithm string) bool {
	n, err := HexLength(algorithm)
	if err != nil || len(s) != n {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// String hashes s with the named algorithm and returns lowercase hex.
func String(s, algorithm string) (string, error) {
	h, err := New(algorithm)
	if err != nil {
		return "", err
	}
	_, _ = io.WriteString(h, s)
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

// Reader hashes all bytes from r with the named algorithm.
func Reader(r io.Reader, algorithm string) (string, error) {
	h, err := New(algorithm)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(h, r); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

// Directory hashes a canonical SFTP logical directory path, which always
// starts with the configured SHARE_PREFIX (e.g. "files/mydir").
// Host and container absolute paths must never reach this function.
func Directory(logicalDir, algorithm string) (string, error) {
	if logicalDir == "" || strings.HasPrefix(logicalDir, "/") || logicalDir == "." {
		return "", fmt.Errorf("invalid logical directory %q", logicalDir)
	}
	return String(logicalDir, algorithm)
}

// Filename hashes a file basename (HASH_TARGET=filename).
func Filename(basename, algorithm string) (string, error) {
	if basename == "" || strings.Contains(basename, "/") {
		return "", fmt.Errorf("invalid basename %q", basename)
	}
	return String(basename, algorithm)
}
