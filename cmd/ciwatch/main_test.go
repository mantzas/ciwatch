package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mantzas/ciwatch/internal/config"
	ghapi "github.com/mantzas/ciwatch/internal/github"
	"github.com/mantzas/ciwatch/internal/state"
	"github.com/mantzas/ciwatch/internal/tui"
)

func TestRunVersion(t *testing.T) {
	code, stdout, stderr := captureRun(t, []string{"--version"})
	if code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}
	if strings.TrimSpace(stdout) != "ciwatch 0.1.0" {
		t.Fatalf("stdout = %q", stdout)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestRunRejectsUnexpectedArgument(t *testing.T) {
	code, _, stderr := captureRun(t, []string{"extra"})
	if code != 2 {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stderr, "unexpected argument: extra") {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestRunMissingConfigPrintsSample(t *testing.T) {
	missing := t.TempDir() + "/missing.toml"
	code, _, stderr := captureRun(t, []string{"--config", missing})
	if code != 1 {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stderr, "config not found") || !strings.Contains(stderr, `repos = ["owner/repo"]`) {
		t.Fatalf("stderr missing config help: %q", stderr)
	}
}

func TestRunPrintConfigPaths(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ciwatch.toml")
	code, stdout, stderr := captureRun(t, []string{"--config", path, "--print-config-paths"})
	if code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}
	if strings.TrimSpace(stdout) != path {
		t.Fatalf("stdout = %q", stdout)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestRunDoctorReportsConfigFailure(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.toml")
	code, stdout, stderr := captureRun(t, []string{"--config", missing, "--doctor"})
	if code != 1 {
		t.Fatalf("code = %d, stdout = %q stderr = %q", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "config: FAIL") || !strings.Contains(stdout, "config path: "+missing) {
		t.Fatalf("stdout = %q", stdout)
	}
	if !strings.Contains(stdout, "cache path:") {
		t.Fatalf("stdout missing cache path: %q", stdout)
	}
}

func TestRunOnceModeUsesInjectedDependencies(t *testing.T) {
	restoreGlobals(t)
	loadConfig = func(path string) (config.Config, config.LoadMeta, error) {
		if path != "custom.toml" {
			t.Fatalf("config path = %q", path)
		}
		return config.Config{Repos: []string{"a/b"}, PollInterval: time.Minute, RunsPerRepo: 1}, config.LoadMeta{Path: path}, nil
	}
	tokenFromGH = func(context.Context, ghapi.CommandContext, time.Duration) (string, error) {
		return "token", nil
	}
	loadDefaultState = func() (state.Cache, string, error) {
		return state.New(), "cache.json", errors.New("bad cache")
	}
	newRunner = func(cfg config.Config, client tui.GitHubClient, cache state.Cache, rebuilt bool) tui.Runner {
		if !rebuilt {
			t.Fatal("runner should be told cache was rebuilt")
		}
		return &onceRunner{snapshot: tui.Snapshot{Rows: []tui.Row{{
			Repo: "a/b", Workflow: "CI", Status: tui.StatusOK, Branch: "main",
		}}}}
	}

	code, stdout, stderr := captureRun(t, []string{"--config", "custom.toml", "--once"})
	if code != 0 {
		t.Fatalf("code = %d stderr = %q", code, stderr)
	}
	if strings.TrimSpace(stdout) != "a/b\tCI\tOK\tmain" {
		t.Fatalf("stdout = %q", stdout)
	}
	if !strings.Contains(stderr, "warning: cache ignored: bad cache") {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestRunReportsGitHubAuthFailure(t *testing.T) {
	restoreGlobals(t)
	loadConfig = func(string) (config.Config, config.LoadMeta, error) {
		return config.Config{Repos: []string{"a/b"}, PollInterval: time.Minute, RunsPerRepo: 1}, config.LoadMeta{}, nil
	}
	tokenFromGH = func(context.Context, ghapi.CommandContext, time.Duration) (string, error) {
		return "", errors.New("no gh")
	}

	code, stdout, stderr := captureRun(t, nil)
	if code != 1 {
		t.Fatalf("code = %d stdout = %q stderr = %q", code, stdout, stderr)
	}
	if stdout != "" || !strings.Contains(stderr, "github auth failed: no gh") {
		t.Fatalf("stdout = %q stderr = %q", stdout, stderr)
	}
}

func TestRunDoctorSuccess(t *testing.T) {
	restoreGlobals(t)
	loadConfig = func(path string) (config.Config, config.LoadMeta, error) {
		return config.Config{Repos: []string{"a/b"}, PollInterval: time.Minute, RunsPerRepo: 1}, config.LoadMeta{Path: "ok.toml"}, nil
	}
	tokenFromGH = func(context.Context, ghapi.CommandContext, time.Duration) (string, error) {
		return "token", nil
	}
	loadDefaultState = func() (state.Cache, string, error) {
		cache := state.New()
		cache.SchemaVersion = 7
		return cache, "cache.json", nil
	}

	code, stdout, stderr := captureRun(t, []string{"--doctor"})
	if code != 0 {
		t.Fatalf("code = %d stdout = %q stderr = %q", code, stdout, stderr)
	}
	for _, want := range []string{"config: OK ok.toml", "github auth: OK", "cache path: cache.json", "cache: OK", "cache schema: 7"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("stdout missing %q: %q", want, stdout)
		}
	}
	if stderr != "" {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestFormatRow(t *testing.T) {
	got := formatRow(tui.Row{Repo: "a/b", Workflow: "CI", Status: tui.StatusBroken, Branch: "main", Title: "failed"})
	want := "a/b\tCI\tBROKEN\tmain\tfailed"
	if got != want {
		t.Fatalf("formatRow = %q, want %q", got, want)
	}
	got = formatRow(tui.Row{Repo: "a/b", Workflow: "-", Status: tui.StatusError, Error: "boom"})
	want = "a/b\t-\tERROR\tboom"
	if got != want {
		t.Fatalf("formatRow error = %q, want %q", got, want)
	}
}

func TestRunOncePrintsRowsAndReportsRefreshError(t *testing.T) {
	row := tui.Row{Repo: "a/b", Workflow: "CI", Status: tui.StatusBroken, Branch: "main", Title: "failed"}
	code, stdout, stderr := captureOutput(t, func() int {
		return runOnce(&onceRunner{snapshot: tui.Snapshot{Rows: []tui.Row{row}}})
	})
	if code != 0 {
		t.Fatalf("code = %d stderr = %q", code, stderr)
	}
	if strings.TrimSpace(stdout) != "a/b\tCI\tBROKEN\tmain\tfailed" {
		t.Fatalf("stdout = %q", stdout)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q", stderr)
	}

	code, stdout, stderr = captureOutput(t, func() int {
		return runOnce(&onceRunner{err: errors.New("boom")})
	})
	if code != 1 {
		t.Fatalf("code = %d stdout = %q stderr = %q", code, stdout, stderr)
	}
	if stdout != "" || !strings.Contains(stderr, "refresh failed: boom") {
		t.Fatalf("stdout = %q stderr = %q", stdout, stderr)
	}
}

func captureRun(t *testing.T, args []string) (int, string, string) {
	t.Helper()
	return captureOutput(t, func() int { return run(args) })
}

func captureOutput(t *testing.T, fn func() int) (int, string, string) {
	t.Helper()
	oldStdout, oldStderr := os.Stdout, os.Stderr
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout, os.Stderr = stdoutW, stderrW
	defer func() {
		os.Stdout, os.Stderr = oldStdout, oldStderr
	}()

	code := fn()
	_ = stdoutW.Close()
	_ = stderrW.Close()
	stdout, err := io.ReadAll(stdoutR)
	if err != nil {
		t.Fatal(err)
	}
	stderr, err := io.ReadAll(stderrR)
	if err != nil {
		t.Fatal(err)
	}
	return code, string(bytes.TrimRight(stdout, "\x00")), string(bytes.TrimRight(stderr, "\x00"))
}

type onceRunner struct {
	snapshot tui.Snapshot
	err      error
}

func (r *onceRunner) Refresh(context.Context) (tui.Snapshot, error) {
	if r.err != nil {
		return tui.Snapshot{}, r.err
	}
	return r.snapshot, nil
}

func (r *onceRunner) Cache() state.Cache {
	return state.New()
}

func restoreGlobals(t *testing.T) {
	t.Helper()
	origLoadConfig := loadConfig
	origCheckedConfigPaths := checkedConfigPaths
	origConfigSample := configSample
	origTokenFromGH := tokenFromGH
	origLoadDefaultState := loadDefaultState
	origNewRunner := newRunner
	t.Cleanup(func() {
		loadConfig = origLoadConfig
		checkedConfigPaths = origCheckedConfigPaths
		configSample = origConfigSample
		tokenFromGH = origTokenFromGH
		loadDefaultState = origLoadDefaultState
		newRunner = origNewRunner
	})
}
