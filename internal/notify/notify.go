package notify

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"time"
)

type CommandContext func(context.Context, string, ...string) *exec.Cmd

type Notification struct {
	Repo     string
	Workflow string
	Branch   string
	Title    string
}

type Service struct {
	goos    string
	command CommandContext
}

func New(goos string, command CommandContext) Service {
	if command == nil {
		command = exec.CommandContext
	}
	return Service{goos: goos, command: command}
}

func (s Service) MacOS(enabled bool, n Notification) error {
	if !enabled || s.goos != "darwin" {
		return nil
	}
	body := n.Workflow + " on " + n.Branch
	if n.Title != "" {
		body += ": " + n.Title
	}
	script := `display notification ` + osaQuote(body) + ` with title ` + osaQuote("ciwatch: "+n.Repo)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return s.command(ctx, "osascript", "-e", script).Run()
}

func (s Service) Open(url string) error {
	if url == "" {
		return errors.New("no url")
	}
	cmd := "xdg-open"
	args := []string{url}
	switch s.goos {
	case "darwin":
		cmd = "open"
	case "windows":
		cmd = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", url}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return s.command(ctx, cmd, args...).Start()
}

func osaQuote(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `\"`) + `"`
}
