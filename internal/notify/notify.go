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
	if !enabled {
		return nil
	}
	body := n.Workflow + " on " + n.Branch
	if n.Title != "" {
		body += ": " + n.Title
	}
	title := "ciwatch: " + n.Repo
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	switch s.goos {
	case "darwin":
		script := `display notification ` + osaQuote(body) + ` with title ` + osaQuote(title)
		return s.command(ctx, "osascript", "-e", script).Run()
	case "linux":
		return s.command(ctx, "notify-send", title, body).Run()
	case "windows":
		script := windowsToastScript(title, body)
		return s.command(ctx, "powershell", "-NoProfile", "-Command", script).Run()
	default:
		return nil
	}
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

func windowsToastScript(title, body string) string {
	// BurntToast is intentionally not required. This uses the built-in toast APIs
	// available to normal PowerShell sessions on supported Windows desktops.
	return strings.Join([]string{
		"[Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime] > $null",
		"$template = [Windows.UI.Notifications.ToastTemplateType]::ToastText02",
		"$xml = [Windows.UI.Notifications.ToastNotificationManager]::GetTemplateContent($template)",
		"$text = $xml.GetElementsByTagName('text')",
		"$text.Item(0).AppendChild($xml.CreateTextNode(" + psQuote(title) + ")) > $null",
		"$text.Item(1).AppendChild($xml.CreateTextNode(" + psQuote(body) + ")) > $null",
		"$toast = [Windows.UI.Notifications.ToastNotification]::new($xml)",
		"[Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier('ciwatch').Show($toast)",
	}, "; ")
}

func osaQuote(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `\"`) + `"`
}

func psQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}
