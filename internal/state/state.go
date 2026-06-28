package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const SchemaVersion = 1

type Cache struct {
	SchemaVersion int               `json:"schema_version"`
	NotifiedRuns  map[string]bool   `json:"notified_runs"`
	ETags         map[string]string `json:"etags"`
	Baseline      bool              `json:"baseline"`
}

func New() Cache {
	return Cache{
		SchemaVersion: SchemaVersion,
		NotifiedRuns:  map[string]bool{},
		ETags:         map[string]string{},
	}
}

func Load(path string) (Cache, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return New(), err
	}
	var cache Cache
	if err := json.Unmarshal(data, &cache); err != nil {
		return New(), err
	}
	if cache.SchemaVersion != SchemaVersion {
		return New(), fmt.Errorf("unsupported cache schema %d", cache.SchemaVersion)
	}
	if cache.NotifiedRuns == nil {
		cache.NotifiedRuns = map[string]bool{}
	}
	if cache.ETags == nil {
		cache.ETags = map[string]string{}
	}
	return cache, nil
}

func LoadDefault() (Cache, string, error) {
	path := DefaultPath()
	cache, err := Load(path)
	if err == nil {
		return cache, path, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return New(), path, err
	}
	return New(), path, err
}

func Save(path string, cache Cache) error {
	cache.SchemaVersion = SchemaVersion
	if cache.NotifiedRuns == nil {
		cache.NotifiedRuns = map[string]bool{}
	}
	if cache.ETags == nil {
		cache.ETags = map[string]string{}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

func DefaultPath() string {
	cacheHome := os.Getenv("XDG_CACHE_HOME")
	if cacheHome == "" {
		if home, err := os.UserHomeDir(); err == nil {
			cacheHome = filepath.Join(home, ".cache")
		}
	}
	if cacheHome == "" {
		return filepath.Join(".", ".ciwatch-state.json")
	}
	return filepath.Join(cacheHome, "ciwatch", "state.json")
}

func RepoKey(repo string) string {
	return strings.ToLower(repo)
}

func AttemptKey(repo string, runID, attempt int64) string {
	return fmt.Sprintf("%s:%d:%d", RepoKey(repo), runID, attempt)
}

func (c *Cache) MarkNotified(repo string, runID, attempt int64) {
	if c.NotifiedRuns == nil {
		c.NotifiedRuns = map[string]bool{}
	}
	c.NotifiedRuns[AttemptKey(repo, runID, attempt)] = true
}

func (c Cache) WasNotified(repo string, runID, attempt int64) bool {
	return c.NotifiedRuns[AttemptKey(repo, runID, attempt)]
}
