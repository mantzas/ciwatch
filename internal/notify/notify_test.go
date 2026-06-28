package notify

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestNotificationsUsePlatformCommand(t *testing.T) {
	tests := map[string]string{
		"darwin":  "osascript",
		"linux":   "notify-send",
		"windows": "powershell",
	}
	for goos, want := range tests {
		t.Run(goos, func(t *testing.T) {
			var calls []string
			var gotArgs [][]string
			svc := New(goos, func(ctx context.Context, name string, args ...string) *exec.Cmd {
				calls = append(calls, name)
				gotArgs = append(gotArgs, args)
				return exec.CommandContext(ctx, "true")
			})
			if err := svc.MacOS(true, Notification{Repo: "a/b", Workflow: "CI", Branch: "main", Title: "broken"}); err != nil {
				t.Fatal(err)
			}
			if len(calls) != 1 || calls[0] != want {
				t.Fatalf("calls = %v, want %s", calls, want)
			}
			if goos == "linux" && (gotArgs[0][0] != "ciwatch: a/b" || gotArgs[0][1] != "CI on main: broken") {
				t.Fatalf("linux args = %v", gotArgs[0])
			}
			if goos == "windows" && !strings.Contains(strings.Join(gotArgs[0], " "), "ciwatch: a/b") {
				t.Fatalf("windows args = %v", gotArgs[0])
			}
		})
	}
}

func TestNotificationsSkipDisabledAndUnsupportedPlatforms(t *testing.T) {
	calls := 0
	command := func(ctx context.Context, name string, args ...string) *exec.Cmd {
		calls++
		return exec.CommandContext(ctx, "true")
	}
	if err := New("linux", command).MacOS(false, Notification{Repo: "a/b"}); err != nil {
		t.Fatal(err)
	}
	if err := New("plan9", command).MacOS(true, Notification{Repo: "a/b"}); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatalf("unexpected notification command calls: %d", calls)
	}
}

func TestOpenUsesPlatformCommand(t *testing.T) {
	var calls []string
	svc := New("darwin", func(ctx context.Context, name string, args ...string) *exec.Cmd {
		calls = append(calls, name)
		return exec.CommandContext(ctx, "true")
	})
	if err := svc.MacOS(true, Notification{Repo: "a/b", Workflow: "CI", Branch: "main", Title: "broken"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.Open("https://github.com/a/b"); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 || calls[0] != "osascript" || calls[1] != "open" {
		t.Fatalf("unexpected calls: %v", calls)
	}
}

func TestOpenWaitsForCommandCompletion(t *testing.T) {
	marker := t.TempDir() + "/opened"
	svc := New("darwin", func(ctx context.Context, name string, args ...string) *exec.Cmd {
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=TestHelperOpenCommand", "--", marker)
		cmd.Env = append(os.Environ(), "CIWATCH_HELPER_OPEN=1")
		return cmd
	})
	if err := svc.Open("https://github.com/a/b"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("open returned before command completed: %v", err)
	}
}

func TestHelperOpenCommand(t *testing.T) {
	if os.Getenv("CIWATCH_HELPER_OPEN") != "1" {
		return
	}
	if len(os.Args) == 0 {
		os.Exit(2)
	}
	marker := os.Args[len(os.Args)-1]
	time.Sleep(50 * time.Millisecond)
	if err := os.WriteFile(marker, []byte("opened\n"), 0o600); err != nil {
		os.Exit(2)
	}
	os.Exit(0)
}

func TestOpenErrorsAndPlatformCommands(t *testing.T) {
	if err := New("linux", nil).Open(""); err == nil {
		t.Fatal("expected no url error")
	}

	tests := map[string]struct {
		want string
		args []string
	}{
		"linux":   {want: "xdg-open", args: []string{"https://example.test"}},
		"windows": {want: "rundll32", args: []string{"url.dll,FileProtocolHandler", "https://example.test"}},
	}
	for goos, tt := range tests {
		t.Run(goos, func(t *testing.T) {
			var call string
			var gotArgs []string
			svc := New(goos, func(ctx context.Context, name string, args ...string) *exec.Cmd {
				call = name
				gotArgs = append([]string(nil), args...)
				return exec.CommandContext(ctx, "true")
			})
			if err := svc.Open("https://example.test"); err != nil {
				t.Fatal(err)
			}
			if call != tt.want || strings.Join(gotArgs, "\x00") != strings.Join(tt.args, "\x00") {
				t.Fatalf("call = %s args = %v, want %s %v", call, gotArgs, tt.want, tt.args)
			}
		})
	}
}

func TestWindowsToastScriptQuotesSingleQuotes(t *testing.T) {
	script := windowsToastScript("ciwatch: a/b", "workflow's branch")
	if !strings.Contains(script, "'workflow''s branch'") {
		t.Fatalf("script did not quote PowerShell string: %s", script)
	}
}
