package sftpadmin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func tempStore(t *testing.T) *Store {
	t.Helper()
	return NewStore(filepath.Join(t.TempDir(), "sub", "users.json"))
}

func TestStoreRoundTrip(t *testing.T) {
	st := tempStore(t)
	kp, err := GenerateEd25519KeyPair("alice")
	if err != nil {
		t.Fatal(err)
	}
	canon, _, err := ValidatePublicKey(kp.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	err = st.Update(func(m *Manifest) error {
		m.Users = append(m.Users, User{Username: "alice", Enabled: true, AuthorizedKeys: []string{canon}})
		return nil
	})
	if err != nil {
		t.Fatalf("Update = %v", err)
	}
	m, err := st.Load()
	if err != nil {
		t.Fatalf("Load = %v", err)
	}
	if m.Version != ManifestVersion || len(m.Users) != 1 || !m.Users[0].Enabled {
		t.Fatalf("unexpected manifest: %+v", m)
	}
	st2, err := os.Stat(st.Path())
	if err != nil {
		t.Fatal(err)
	}
	if st2.Mode().Perm() != 0o600 {
		t.Errorf("manifest mode = %o, want 600", st2.Mode().Perm())
	}
}

func TestStoreAbortedUpdateKeepsFile(t *testing.T) {
	st := tempStore(t)
	if err := st.Update(func(m *Manifest) error {
		m.Users = append(m.Users, User{Username: "alice", Enabled: true})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(st.Path())
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Update(func(_ *Manifest) error { return errTestBoom }); err == nil {
		t.Fatal("Update with failing fn = nil, want error")
	}
	after, err := os.ReadFile(st.Path())
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Error("failed update modified the manifest file")
	}
}

var errTestBoom = errBoom{}

type errBoom struct{}

func (errBoom) Error() string { return "boom" }

func TestStoreRejectsBadDocuments(t *testing.T) {
	st := tempStore(t)
	if err := os.MkdirAll(filepath.Dir(st.Path()), 0o700); err != nil {
		t.Fatal(err)
	}
	write := func(doc string) {
		t.Helper()
		if err := os.WriteFile(st.Path(), []byte(doc), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for name, doc := range map[string]string{
		"bad version": `{"version":2,"users":[]}`,
		"bad json":    `{not json`,
		"reserved":    `{"version":1,"users":[{"username":"root","enabled":true}]}`,
		"bad user":    `{"version":1,"users":[{"username":"UP","enabled":true}]}`,
		"dup":         `{"version":1,"users":[{"username":"a","enabled":true},{"username":"a","enabled":true}]}`,
		"bad key":     `{"version":1,"users":[{"username":"a","enabled":true,"authorized_keys":["nope"]}]}`,
	} {
		write(doc)
		if _, err := st.Load(); err == nil {
			t.Errorf("%s: Load = nil, want error", name)
		}
		if err := st.Update(func(_ *Manifest) error { return nil }); err == nil {
			t.Errorf("%s: Update passthrough = nil, want error", name)
		}
	}
}

func TestStoreConcurrentReadersSeeCompleteDocs(t *testing.T) {
	st := tempStore(t)
	if err := st.Update(func(m *Manifest) error {
		m.Users = append(m.Users, User{Username: "seed", Enabled: true})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 64)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				raw, err := os.ReadFile(st.Path())
				if err != nil {
					errs <- err
					return
				}
				var m Manifest
				if err := json.Unmarshal(raw, &m); err != nil {
					errs <- err
					return
				}
				if m.Version != ManifestVersion {
					errs <- errBoom{}
					return
				}
			}
		}(i)
	}
	for j := 0; j < 10; j++ {
		name := "user" + string(rune('a'+j))
		if err := st.Update(func(m *Manifest) error {
			m.Users = append(m.Users, User{Username: name, Enabled: true})
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("reader saw partial document: %v", err)
	}
	if leftovers, _ := filepath.Glob(filepath.Join(filepath.Dir(st.Path()), ".users-*.tmp")); len(leftovers) != 0 {
		t.Errorf("leftover temp files: %v", leftovers)
	}
}
