package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
)

var ErrNotFound = errors.New("config not found")

const (
	DefaultPollInterval = time.Minute
	DefaultRunsPerRepo  = 5
	DefaultNotifyMacOS  = true
	MinPollInterval     = 15 * time.Second
)

type Config struct {
	Repos        []string      `toml:"repos"`
	PollInterval time.Duration `toml:"poll_interval"`
	RunsPerRepo  int           `toml:"runs_per_repo"`
	NotifyMacOS  bool          `toml:"notify_macos"`
}

type loadFile struct {
	Repos        []string `toml:"repos"`
	PollInterval string   `toml:"poll_interval"`
	RunsPerRepo  *int     `toml:"runs_per_repo"`
	NotifyMacOS  *bool    `toml:"notify_macos"`
}

type LoadMeta struct {
	Path         string
	CheckedPaths []string
}

var repoPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

func Load(path string) (Config, LoadMeta, error) {
	paths := lookupPaths(path)
	meta := LoadMeta{CheckedPaths: paths}
	for _, candidate := range paths {
		data, err := os.ReadFile(candidate)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return Config{}, meta, fmt.Errorf("read config %s: %w", candidate, err)
		}
		cfg, err := Parse(data)
		meta.Path = candidate
		if err != nil {
			return Config{}, meta, fmt.Errorf("parse config %s: %w", candidate, err)
		}
		return cfg, meta, nil
	}
	return Config{}, meta, fmt.Errorf("%w; checked: %s", ErrNotFound, strings.Join(paths, ", "))
}

func Parse(data []byte) (Config, error) {
	var file loadFile
	dec := toml.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&file); err != nil {
		return Config{}, err
	}

	cfg := Config{
		Repos:        file.Repos,
		PollInterval: DefaultPollInterval,
		RunsPerRepo:  DefaultRunsPerRepo,
		NotifyMacOS:  DefaultNotifyMacOS,
	}
	if file.PollInterval != "" {
		d, err := time.ParseDuration(file.PollInterval)
		if err != nil {
			return Config{}, fmt.Errorf("poll_interval: %w", err)
		}
		cfg.PollInterval = d
	}
	if file.RunsPerRepo != nil {
		cfg.RunsPerRepo = *file.RunsPerRepo
	}
	if file.NotifyMacOS != nil {
		cfg.NotifyMacOS = *file.NotifyMacOS
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) Validate() error {
	if len(c.Repos) == 0 {
		return errors.New("repos must contain at least one owner/repo")
	}
	seen := map[string]string{}
	for _, repo := range c.Repos {
		if !repoPattern.MatchString(repo) {
			return fmt.Errorf("invalid repo %q; expected owner/repo", repo)
		}
		key := strings.ToLower(repo)
		if prior, ok := seen[key]; ok {
			return fmt.Errorf("duplicate repo %q duplicates %q", repo, prior)
		}
		seen[key] = repo
	}
	if c.PollInterval < MinPollInterval {
		return fmt.Errorf("poll_interval must be at least %s", MinPollInterval)
	}
	if c.RunsPerRepo < 1 || c.RunsPerRepo > 20 {
		return errors.New("runs_per_repo must be between 1 and 20")
	}
	return nil
}

func lookupPaths(path string) []string {
	if path != "" {
		return []string{path}
	}
	paths := []string{filepath.Join(".", "config.toml")}
	configHome := os.Getenv("XDG_CONFIG_HOME")
	if configHome == "" {
		if home, err := os.UserHomeDir(); err == nil {
			configHome = filepath.Join(home, ".config")
		}
	}
	if configHome != "" {
		paths = append(paths, filepath.Join(configHome, "ciwatch", "config.toml"))
	}
	return slices.Compact(paths)
}

func Sample(checked []string) string {
	var b strings.Builder
	b.WriteString("\nChecked paths:\n")
	for _, path := range checked {
		b.WriteString("  - ")
		b.WriteString(path)
		b.WriteByte('\n')
	}
	b.WriteString(`
Sample config:

repos = ["owner/repo"]

# poll_interval = "60s"
# runs_per_repo = 5
# notify_macos = true
`)
	return b.String()
}
