package hash

import (
	"strings"
	"testing"
)

func TestKnownDigests(t *testing.T) {
	dirMD5, err := Directory("files/mydir", MD5)
	if err != nil {
		t.Fatal(err)
	}
	if len(dirMD5) != 32 {
		t.Fatalf("md5 length: %q", dirMD5)
	}
	nameMD5, err := Filename("cron.txt", MD5)
	if err != nil {
		t.Fatal(err)
	}
	if len(nameMD5) != 32 {
		t.Fatalf("md5 length: %q", nameMD5)
	}
	dirSHA, err := Directory("files/mydir", SHA256)
	if err != nil {
		t.Fatal(err)
	}
	if len(dirSHA) != 64 || dirMD5 == dirSHA {
		t.Fatalf("sha256 digest unexpected: %q", dirSHA)
	}
	// Lowercase hex only.
	if dirMD5 != strings.ToLower(dirMD5) {
		t.Fatalf("digest not lowercase: %q", dirMD5)
	}
}

func TestContentHash(t *testing.T) {
	h1, err := Reader(strings.NewReader("hello"), MD5)
	if err != nil {
		t.Fatal(err)
	}
	if h1 != "5d41402abc4b2a76b9719d911017c592" {
		t.Fatalf("content md5: %q", h1)
	}
	h2, err := Reader(strings.NewReader("hello"), SHA256)
	if err != nil {
		t.Fatal(err)
	}
	if len(h2) != 64 {
		t.Fatalf("content sha256 length: %q", h2)
	}
}

func TestPrefixIncludedInDirectoryHash(t *testing.T) {
	withPrefix, err := Directory("files/mydir", MD5)
	if err != nil {
		t.Fatal(err)
	}
	withoutPrefix, err := Directory("mydir", MD5)
	if err != nil {
		t.Fatal(err)
	}
	if withPrefix == withoutPrefix {
		t.Fatal("directory hash must include SHARE_PREFIX")
	}
	// Absolute host/container paths are rejected as hash input.
	for _, bad := range []string{"/opt/sharefiles/files/mydir", "/home/linebot/files/mydir", ".", ""} {
		if _, err := Directory(bad, MD5); err == nil {
			t.Fatalf("expected error for %q", bad)
		}
	}
}

func TestValidHex(t *testing.T) {
	if !ValidHex(strings.Repeat("a", 32), MD5) {
		t.Fatal("valid md5 rejected")
	}
	if ValidHex(strings.Repeat("a", 64), MD5) {
		t.Fatal("sha256 digest accepted as md5")
	}
	if !ValidHex(strings.Repeat("b", 64), SHA256) {
		t.Fatal("valid sha256 rejected")
	}
	if ValidHex(strings.Repeat("A", 32), MD5) {
		t.Fatal("uppercase digest accepted")
	}
	if ValidHex("xyz", MD5) {
		t.Fatal("non-hex accepted")
	}
}
