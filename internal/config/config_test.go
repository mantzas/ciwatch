package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseDefaultsAndValidation(t *testing.T) {
	cfg, err := Parse([]byte(`repos = ["Mantzas/Repo"]`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PollInterval != DefaultPollInterval || cfg.RunsPerRepo != DefaultRunsPerRepo || !cfg.NotifyMacOS {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	if cfg.Repos[0] != "Mantzas/Repo" {
		t.Fatalf("casing was not preserved: %q", cfg.Repos[0])
	}
}

func TestParseRejectsUnknownDuplicateInvalidAndBounds(t *testing.T) {
	tests := map[string]string{
		"unknown":   "repos = [\"a/b\"]\nextra = true\n",
		"duplicate": "repos = [\"a/b\", \"A/B\"]\n",
		"invalid":   "repos = [\"nope\"]\n",
		"short":     "repos = [\"a/b\"]\npoll_interval = \"14s\"\n",
		"runs low":  "repos = [\"a/b\"]\nruns_per_repo = 0\n",
		"runs high": "repos = [\"a/b\"]\nruns_per_repo = 21\n",
	}
	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse([]byte(input)); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestLoadLookupOrderAndMissingSample(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "xdg"))
	if err := os.WriteFile(filepath.Join(dir, "chosen.toml"), []byte(`repos = ["a/b"]`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, meta, err := Load(filepath.Join(dir, "chosen.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Repos[0] != "a/b" || meta.Path == "" {
		t.Fatalf("unexpected load: %+v %+v", cfg, meta)
	}
	_, meta, err = Load(filepath.Join(dir, "missing.toml"))
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if sample := Sample(meta.CheckedPaths); !strings.Contains(sample, "repos = [\"owner/repo\"]") {
		t.Fatalf("sample missing repo example: %s", sample)
	}
}

func TestParseExplicitValues(t *testing.T) {
	cfg, err := Parse([]byte("repos = [\"a/b\"]\npoll_interval = \"15s\"\nruns_per_repo = 20\nnotify_macos = false\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PollInterval != 15*time.Second || cfg.RunsPerRepo != 20 || cfg.NotifyMacOS {
		t.Fatalf("unexpected cfg: %+v", cfg)
	}
}
