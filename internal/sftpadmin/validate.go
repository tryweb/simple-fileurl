package sftpadmin

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// usernamePattern is the deployment contract for SFTP login names, shared
// with the SFTP reconciler: lowercase start, max 32 chars.
var usernamePattern = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)

// reservedUsernames can never become SFTP users: they collide with system
// accounts or the SFTP login group.
var reservedUsernames = map[string]bool{
	"root":      true,
	"admin":     true,
	"sshd":      true,
	"sftpusers": true,
}

// ValidateUsername rejects malformed and reserved SFTP usernames.
func ValidateUsername(name string) error {
	if !usernamePattern.MatchString(name) {
		return fmt.Errorf("invalid username %q: must match ^[a-z_][a-z0-9_-]{0,31}$", name)
	}
	if reservedUsernames[name] {
		return fmt.Errorf("invalid username %q: reserved system name", name)
	}
	return nil
}

// allowedKeyTypes are the OpenSSH public key algorithms the SFTP side can
// serve. Generated keys are always ssh-ed25519; the rest cover user-supplied
// keys from their own ssh-keygen runs.
var allowedKeyTypes = map[string]bool{
	"ssh-ed25519":                true,
	"ssh-rsa":                    true,
	"ecdsa-sha2-nistp256":        true,
	"ecdsa-sha2-nistp384":        true,
	"ecdsa-sha2-nistp521":        true,
	"sk-ssh-ed25519@openssh.com": true,
}

// ValidatePublicKey checks single-line OpenSSH public key text and returns
// the canonical "type base64" form plus the OpenSSH-style SHA256 fingerprint.
// Private key material, multi-line input, and unknown types are rejected.
func ValidatePublicKey(key string) (canonical string, fingerprint string, err error) {
	if strings.Contains(key, "\n") || strings.Contains(key, "\r") {
		return "", "", errors.New("invalid public key: must be a single line")
	}
	trimmed := strings.TrimSpace(key)
	if trimmed == "" {
		return "", "", errors.New("invalid public key: empty")
	}
	if strings.HasPrefix(trimmed, "-----BEGIN") {
		return "", "", errors.New("invalid public key: private keys are not accepted")
	}
	parts := strings.Fields(trimmed)
	if len(parts) < 2 {
		return "", "", errors.New("invalid public key: expected '<type> <base64> [comment]'")
	}
	typ, b64 := parts[0], parts[1]
	if !allowedKeyTypes[typ] {
		return "", "", fmt.Errorf("invalid public key: unsupported type %q", typ)
	}
	blob, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return "", "", fmt.Errorf("invalid public key: bad base64: %w", err)
	}
	if err := checkKeyBlob(typ, blob); err != nil {
		return "", "", err
	}
	return typ + " " + b64, "SHA256:" + fingerprintSHA256(blob), nil
}

// checkKeyBlob verifies the decoded wire blob actually encodes a key of the
// declared type instead of arbitrary base64 with a valid-looking prefix.
func checkKeyBlob(typ string, blob []byte) error {
	inner, rest, err := readWireBytes(blob)
	if err != nil {
		return errors.New("invalid public key: truncated blob")
	}
	if string(inner) != typ {
		return fmt.Errorf("invalid public key: wire type %q does not match declared %q", inner, typ)
	}
	switch typ {
	case "ssh-ed25519", "sk-ssh-ed25519@openssh.com":
		k, tail, err := readWireBytes(rest)
		if err != nil || len(k) != 32 || len(tail) != 0 {
			return errors.New("invalid public key: malformed ed25519 key")
		}
	case "ssh-rsa":
		e, tail, err := readWireBytes(rest)
		if err != nil || len(e) == 0 || len(e) > 8 {
			return errors.New("invalid public key: malformed rsa exponent")
		}
		n, tail, err := readWireBytes(tail)
		if err != nil || len(n) < 32 || len(tail) != 0 {
			return errors.New("invalid public key: malformed rsa modulus")
		}
	default: // ecdsa-*: curve name must match the type suffix, key must be present
		curve, tail, err := readWireBytes(rest)
		if err != nil || string(curve) != strings.TrimPrefix(typ, "ecdsa-sha2-") {
			return errors.New("invalid public key: malformed ecdsa curve")
		}
		k, tail, err := readWireBytes(tail)
		if err != nil || len(k) == 0 || len(tail) != 0 {
			return errors.New("invalid public key: malformed ecdsa key")
		}
	}
	return nil
}

// readWireBytes consumes one uint32-length-prefixed field.
func readWireBytes(b []byte) ([]byte, []byte, error) {
	if len(b) < 4 {
		return nil, nil, errors.New("truncated")
	}
	n := binary.BigEndian.Uint32(b)
	if uint64(n) > uint64(len(b)-4) {
		return nil, nil, errors.New("truncated")
	}
	return b[4 : 4+n], b[4+n:], nil
}

// fingerprintSHA256 renders the OpenSSH SHA256 fingerprint of a key blob
// (base64 without padding, matching `ssh-keygen -l -E sha256`).
func fingerprintSHA256(blob []byte) string {
	sum := sha256.Sum256(blob)
	return strings.TrimRight(base64.StdEncoding.EncodeToString(sum[:]), "=")
}
