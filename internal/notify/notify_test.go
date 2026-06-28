package notify

import (
	"context"
	"os/exec"
	"testing"
)

func TestMacOSAndOpenUseInjectedCommand(t *testing.T) {
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
