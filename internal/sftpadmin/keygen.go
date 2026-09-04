package sftpadmin

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// KeyPair is a fresh ed25519 identity: the public half is retained in the
// manifest, the private half is handed out exactly once and never stored.
type KeyPair struct {
	// PublicKey is canonical "ssh-ed25519 AAAA... comment" text.
	PublicKey string
	// PrivatePEM is the standard OPENSSH PRIVATE KEY format accepted by
	// OpenSSH clients via `ssh -i`.
	PrivatePEM string
}

// GenerateEd25519KeyPair creates a fresh identity with the system ssh-keygen.
// The private half is read from a protected temporary directory and never
// retained by the service.
func GenerateEd25519KeyPair(comment string) (KeyPair, error) {
	dir, err := os.MkdirTemp("", "sftp-admin-key-")
	if err != nil {
		return KeyPair{}, fmt.Errorf("create key temp directory: %w", err)
	}
	defer os.RemoveAll(dir)
	if err := os.Chmod(dir, 0o700); err != nil {
		return KeyPair{}, fmt.Errorf("protect key temp directory: %w", err)
	}
	keyPath := filepath.Join(dir, "id_ed25519")
	cmd := exec.Command("ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-C", comment, "-f", keyPath)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		return KeyPair{}, fmt.Errorf("generate ed25519 key: %w", err)
	}
	private, err := os.ReadFile(keyPath)
	if err != nil {
		return KeyPair{}, fmt.Errorf("read generated private key: %w", err)
	}
	public, err := os.ReadFile(keyPath + ".pub")
	if err != nil {
		return KeyPair{}, fmt.Errorf("read generated public key: %w", err)
	}
	return KeyPair{PublicKey: strings.TrimSpace(string(public)), PrivatePEM: string(private)}, nil
}
