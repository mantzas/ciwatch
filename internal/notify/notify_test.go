package notify

import (
	"context"
	"os/exec"
	"strings"
	"testing"
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
