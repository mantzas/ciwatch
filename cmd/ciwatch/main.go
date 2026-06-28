package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mantzas/ciwatch/internal/config"
	ghapi "github.com/mantzas/ciwatch/internal/github"
	"github.com/mantzas/ciwatch/internal/notify"
	"github.com/mantzas/ciwatch/internal/state"
	"github.com/mantzas/ciwatch/internal/tui"
)

const version = "0.1.0"

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	fs := flag.NewFlagSet("ciwatch", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	configPath := fs.String("config", "", "path to config file")
	showVersion := fs.Bool("version", false, "print version")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(os.Stderr, "unexpected argument: %s\n", fs.Arg(0))
		return 2
	}
	if *showVersion {
		fmt.Printf("ciwatch %s\n", version)
		return 0
	}

	cfg, meta, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		if errors.Is(err, config.ErrNotFound) {
			fmt.Fprintln(os.Stderr, config.Sample(meta.CheckedPaths))
		}
		return 1
	}

	token, err := ghapi.TokenFromGH(context.Background(), exec.CommandContext, 10*time.Second)
	if err != nil {
		fmt.Fprintf(os.Stderr, "github auth failed: %v\nrun `gh auth login` and ensure `gh` is on PATH\n", err)
		return 1
	}

	cache, cachePath, cacheWarn := state.LoadDefault()
	if cacheWarn != nil {
		fmt.Fprintf(os.Stderr, "warning: cache ignored: %v\n", cacheWarn)
	}

	client := ghapi.NewClient(token, &http.Client{Timeout: 15 * time.Second})
	runner := tui.NewRunner(cfg, client, cache, cacheWarn != nil)
	app := tui.NewModel(cfg, runner, notify.New(runtime.GOOS, exec.CommandContext), cachePath)

	program := tea.NewProgram(app, tea.WithAltScreen())
	finalModel, err := program.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "tui failed: %v\n", err)
		return 1
	}

	if m, ok := finalModel.(tui.Model); ok {
		if err := m.SaveState(); err != nil {
			fmt.Fprintf(os.Stderr, "warning: cache save failed: %v\n", err)
		}
	}
	return 0
}
