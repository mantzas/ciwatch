package state

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
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

func TestLoadInitializesMissingMaps(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte(`{"schema_version":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cache, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	cache.MarkNotified("A/B", 1, 1)
	if !cache.WasNotified("a/b", 1, 1) {
		t.Fatalf("notified map was not initialized: %+v", cache)
	}
	cache.ETags[RepoKey("A/B")] = `"etag"`
	if cache.ETags["a/b"] != `"etag"` {
		t.Fatalf("etag map was not initialized: %+v", cache)
	}
}

func TestSaveInitializesNilMapsAndCreatesDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "state.json")
	if err := Save(path, Cache{}); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.SchemaVersion != SchemaVersion || loaded.NotifiedRuns == nil || loaded.ETags == nil {
		t.Fatalf("unexpected cache: %+v", loaded)
	}
}

func TestLoadDefaultUsesXDGCacheHome(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", dir)
	cache, path, err := LoadDefault()
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected missing cache, got %v", err)
	}
	if cache.SchemaVersion != SchemaVersion {
		t.Fatalf("unexpected cache: %+v", cache)
	}
	want := filepath.Join(dir, "ciwatch", "state.json")
	if path != want || DefaultPath() != want {
		t.Fatalf("path = %q default = %q want %q", path, DefaultPath(), want)
	}
}

func TestLoadDefaultReturnsInvalidCacheWarning(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", dir)
	path := DefaultPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	cache, gotPath, err := LoadDefault()
	if err == nil || !strings.Contains(err.Error(), "unexpected end") {
		t.Fatalf("expected corrupt cache error, got %v", err)
	}
	if gotPath != path || cache.SchemaVersion != SchemaVersion {
		t.Fatalf("path/cache = %q %+v", gotPath, cache)
	}
}

func TestLoadDefaultSuccessAndNilMapMarkNotified(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", dir)
	path := DefaultPath()
	cache := New()
	cache.ETags["a/b"] = `"etag"`
	if err := Save(path, cache); err != nil {
		t.Fatal(err)
	}
	loaded, gotPath, err := LoadDefault()
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != path || loaded.ETags["a/b"] != `"etag"` {
		t.Fatalf("LoadDefault = %+v %q", loaded, gotPath)
	}

	var empty Cache
	empty.MarkNotified("A/B", 1, 2)
	if !empty.WasNotified("a/b", 1, 2) {
		t.Fatalf("MarkNotified did not initialize nil map: %+v", empty)
	}
}
