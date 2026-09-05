package links

import (
	"sync"
	"testing"
	"time"
)

func testLink(id string) Link {
	return Link{
		ID:          id,
		Scope:       Scope{Type: ScopeUser, User: "jonathan"},
		CreatedAt:   time.Now().UTC().Truncate(time.Second),
		CreatedBy:   "admin",
		Description: "test link",
	}
}

func TestCRUDRoundTrip(t *testing.T) {
	dir := t.TempDir()
	st := NewStore(dir)
	if err := st.Load(); err != nil {
		t.Fatal(err)
	}
	if err := st.Create(testLink("abc123")); err != nil {
		t.Fatal(err)
	}
	if err := st.Create(testLink("abc123")); err != ErrExists {
		t.Fatalf("duplicate create: %v", err)
	}
	got, ok := st.Get("abc123")
	if !ok || got.Description != "test link" {
		t.Fatalf("get: %+v %v", got, ok)
	}
	if _, ok := st.Get("missing"); ok {
		t.Fatal("get unknown: expected miss")
	}
	if err := st.Update("abc123", func(l *Link) error {
		l.Description = "updated"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.Update("missing", func(l *Link) error { return nil }); err != ErrNotFound {
		t.Fatalf("update unknown: %v", err)
	}
	// Persistence across instances.
	st2 := NewStore(dir)
	if err := st2.Load(); err != nil {
		t.Fatal(err)
	}
	got, ok = st2.Get("abc123")
	if !ok || got.Description != "updated" {
		t.Fatalf("reload: %+v %v", got, ok)
	}
	if err := st2.Delete("abc123"); err != nil {
		t.Fatal(err)
	}
	if _, ok := st2.Get("abc123"); ok {
		t.Fatal("delete: still present")
	}
}

func TestExpiredPurgedOnWrite(t *testing.T) {
	dir := t.TempDir()
	st := NewStore(dir)
	if err := st.Load(); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-time.Hour).UTC()
	future := time.Now().Add(time.Hour).UTC()
	old := testLink("old")
	old.ExpiresAt = &past
	fresh := testLink("fresh")
	fresh.ExpiresAt = &future
	never := testLink("never")
	for _, l := range []Link{old, fresh, never} {
		if err := st.Create(l); err != nil {
			t.Fatal(err)
		}
	}
	if !old.Expired(time.Now()) || fresh.Expired(time.Now()) || never.Expired(time.Now()) {
		t.Fatal("expiry check wrong")
	}
	// Expired entries are purged on the next write.
	if err := st.Create(testLink("trigger")); err != nil {
		t.Fatal(err)
	}
	if _, ok := st.Get("old"); ok {
		t.Fatal("expired link not purged")
	}
	if _, ok := st.Get("fresh"); !ok {
		t.Fatal("live link purged")
	}
}

func TestConcurrentCreates(t *testing.T) {
	dir := t.TempDir()
	st := NewStore(dir)
	if err := st.Load(); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id, err := NewID()
			if err != nil {
				t.Error(err)
				return
			}
			if err := st.Create(testLink(id)); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	if n := len(st.List()); n != 20 {
		t.Fatalf("stored %d, want 20", n)
	}
}

func TestPasswordRoundTrip(t *testing.T) {
	hash, err := HashPassword("s3cret!")
	if err != nil {
		t.Fatal(err)
	}
	if !CheckPassword(hash, "s3cret!") {
		t.Fatal("correct password rejected")
	}
	if CheckPassword(hash, "wrong") {
		t.Fatal("wrong password accepted")
	}
	if CheckPassword("", "anything") {
		t.Fatal("empty hash accepted")
	}
}

func TestNewIDFormat(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		id, err := NewID()
		if err != nil {
			t.Fatal(err)
		}
		if len(id) != 16 {
			t.Fatalf("id length %d: %q", len(id), id)
		}
		for _, r := range id {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
				t.Fatalf("non-url-safe char in %q", id)
			}
		}
		if seen[id] {
			t.Fatalf("duplicate id %q", id)
		}
		seen[id] = true
	}
}

func TestScopeValidate(t *testing.T) {
	if err := (Scope{Type: ScopeAdmin}).Validate(); err != nil {
		t.Fatalf("admin: %v", err)
	}
	if err := (Scope{Type: ScopeAdmin, User: "x"}).Validate(); err == nil {
		t.Fatal("admin with user: expected error")
	}
	if err := (Scope{Type: ScopeUser, User: "jonathan"}).Validate(); err != nil {
		t.Fatalf("user: %v", err)
	}
	for _, bad := range []Scope{
		{Type: ScopeUser, User: "Root"},
		{Type: ScopeUser, User: ""},
		{Type: "group", User: "jonathan"},
	} {
		if err := bad.Validate(); err == nil {
			t.Fatalf("%+v: expected error", bad)
		}
	}
}
