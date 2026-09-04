package sftpadmin

import (
	"strings"
	"testing"
)

func TestValidateUsername(t *testing.T) {
	valid := []string{"alice", "bob_1", "_svc", "a", "u-9_x", "abcdefghij1234567890abcdefghij12"}
	for _, n := range valid {
		if err := ValidateUsername(n); err != nil {
			t.Errorf("ValidateUsername(%q) = %v, want nil", n, err)
		}
	}
	invalid := []string{
		"", "Root", "ALICE", "0abc", "-bad", "with space", "a.b",
		"root", "admin", "sshd", "sftpusers", // reserved
		"abcdefghijklmnopqrstuvwxyzabcdefg", // 33 chars
		"../x", "x/y",
	}
	for _, n := range invalid {
		if err := ValidateUsername(n); err == nil {
			t.Errorf("ValidateUsername(%q) = nil, want error", n)
		}
	}
}

func TestValidatePublicKeyRoundTrip(t *testing.T) {
	kp, err := GenerateEd25519KeyPair("alice")
	if err != nil {
		t.Fatal(err)
	}
	canon, fp, err := ValidatePublicKey(kp.PublicKey)
	if err != nil {
		t.Fatalf("ValidatePublicKey(generated) = %v", err)
	}
	if !strings.HasPrefix(canon, "ssh-ed25519 ") {
		t.Errorf("canonical = %q, want ssh-ed25519 prefix", canon)
	}
	if !strings.HasPrefix(fp, "SHA256:") || strings.HasSuffix(fp, "=") {
		t.Errorf("fingerprint = %q, want unpadded SHA256: form", fp)
	}
	// Idempotent: canonical form revalidates to itself with same fingerprint.
	canon2, fp2, err := ValidatePublicKey(canon + " extra-comment")
	if err != nil {
		t.Fatalf("revalidate = %v", err)
	}
	if canon2 != canon || fp2 != fp {
		t.Errorf("revalidate = (%q,%q), want (%q,%q)", canon2, fp2, canon, fp)
	}
}

func TestValidatePublicKeyRejects(t *testing.T) {
	kp, err := GenerateEd25519KeyPair("bob")
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"empty":      "",
		"private":    "-----BEGIN PRIVATE KEY-----\nabc\n-----END PRIVATE KEY-----",
		"multiline":  "ssh-ed25519 AAAA\nssh-ed25519 BBBB",
		"carriage":   "ssh-ed25519 AAAA\r",
		"one field":  "ssh-ed25519",
		"bad type":   "ssh-dss AAAAC3NzaC1kc3MAAA== x",
		"bad base64": "ssh-ed25519 !!!not-base64!!! x",
		"truncated":  "ssh-ed25519 AAAACw== x",
		"mismatch":   "ssh-rsa " + strings.Split(kp.PublicKey, " ")[1] + " x",
	}
	for name, input := range cases {
		if _, _, err := ValidatePublicKey(input); err == nil {
			t.Errorf("%s: got nil error, want rejection", name)
		}
	}
	// Private key PEM body must never validate even without header lines.
	if _, _, err := ValidatePublicKey(kp.PrivatePEM); err == nil {
		t.Error("private PEM validated as public key, want rejection")
	}
}

func TestGeneratedKeyMatchesWireContract(t *testing.T) {
	kp, err := GenerateEd25519KeyPair("carol")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(kp.PrivatePEM, "PRIVATE KEY") {
		t.Error("private PEM missing PRIVATE KEY block")
	}
	if _, _, err := ValidatePublicKey(kp.PublicKey); err != nil {
		t.Errorf("generated public key invalid: %v", err)
	}
}
