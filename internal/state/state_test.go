package state

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveLoadAndDedupe(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	cache := New()
	cache.ETags[RepoKey("A/B")] = `"etag"`
	cache.MarkNotified("A/B", 10, 2)
	cache.Baseline = true
	if err := Save(path, cache); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.SchemaVersion != SchemaVersion || loaded.ETags["a/b"] != `"etag"` || !loaded.WasNotified("a/b", 10, 2) || !loaded.Baseline {
		t.Fatalf("unexpected cache: %+v", loaded)
	}
}

func TestLoadCorruptAndSchema(t *testing.T) {
	dir := t.TempDir()
	corrupt := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(corrupt, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(corrupt); err == nil {
		t.Fatal("expected corrupt error")
	}
	old := filepath.Join(dir, "old.json")
	if err := os.WriteFile(old, []byte(`{"schema_version":99}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(old); err == nil {
		t.Fatal("expected schema error")
	}
}
