package main

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
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

func captureRun(t *testing.T, args []string) (int, string, string) {
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

	code := run(args)
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
