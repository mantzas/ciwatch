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
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mantzas/ciwatch/internal/config"
	ghapi "github.com/mantzas/ciwatch/internal/github"
	"github.com/mantzas/ciwatch/internal/notify"
	"github.com/mantzas/ciwatch/internal/state"
	"github.com/mantzas/ciwatch/internal/tui"
)

const version = "0.1.0"

var (
	loadConfig         = config.Load
	checkedConfigPaths = config.CheckedPaths
	configSample       = config.Sample
	tokenFromGH        = ghapi.TokenFromGH
	loadDefaultState   = state.LoadDefault
	newRunner          = func(cfg config.Config, client tui.GitHubClient, cache state.Cache, rebuilt bool) tui.Runner {
		return tui.NewRunner(cfg, client, cache, rebuilt)
	}
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	fs := flag.NewFlagSet("ciwatch", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	configPath := fs.String("config", "", "path to config file")
	printConfigPaths := fs.Bool("print-config-paths", false, "print checked config paths and exit")
	doctor := fs.Bool("doctor", false, "check config, GitHub auth, and cache path")
	once := fs.Bool("once", false, "fetch once, print a plain text summary, and exit")
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
	if *printConfigPaths {
		for _, path := range checkedConfigPaths(*configPath) {
			fmt.Println(path)
		}
		return 0
	}
	if *doctor {
		return runDoctor(*configPath)
	}

	cfg, meta, err := loadConfig(*configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		if errors.Is(err, config.ErrNotFound) {
			fmt.Fprintln(os.Stderr, configSample(meta.CheckedPaths))
		}
		return 1
	}

	token, err := tokenFromGH(context.Background(), exec.CommandContext, 10*time.Second)
	if err != nil {
		fmt.Fprintf(os.Stderr, "github auth failed: %v\nrun `gh auth login` and ensure `gh` is on PATH\n", err)
		return 1
	}

	cache, cachePath, cacheWarn := loadDefaultState()
	if cacheWarn != nil {
		fmt.Fprintf(os.Stderr, "warning: cache ignored: %v\n", cacheWarn)
	}

	client := ghapi.NewClient(token, &http.Client{Timeout: 15 * time.Second})
	runner := newRunner(cfg, client, cache, cacheWarn != nil)
	if *once {
		return runOnce(runner)
	}
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

func runDoctor(configPath string) int {
	ok := true
	_, meta, err := loadConfig(configPath)
	if err != nil {
		ok = false
		fmt.Printf("config: FAIL %v\n", err)
		for _, path := range meta.CheckedPaths {
			fmt.Printf("config path: %s\n", path)
		}
	} else {
		fmt.Printf("config: OK %s\n", meta.Path)
	}

	if _, err := tokenFromGH(context.Background(), exec.CommandContext, 10*time.Second); err != nil {
		ok = false
		fmt.Printf("github auth: FAIL %v\n", err)
	} else {
		fmt.Println("github auth: OK")
	}

	cache, path, err := loadDefaultState()
	fmt.Printf("cache path: %s\n", path)
	switch {
	case err == nil:
		fmt.Println("cache: OK")
	case errors.Is(err, os.ErrNotExist):
		fmt.Println("cache: MISSING")
	default:
		ok = false
		fmt.Printf("cache: WARN %v\n", err)
	}
	if cache.SchemaVersion != 0 {
		fmt.Printf("cache schema: %d\n", cache.SchemaVersion)
	}
	if !ok {
		return 1
	}
	return 0
}

func runOnce(runner interface {
	Refresh(context.Context) (tui.Snapshot, error)
}) int {
	snapshot, err := runner.Refresh(context.Background())
	if err != nil {
		fmt.Fprintf(os.Stderr, "refresh failed: %v\n", err)
		return 1
	}
	for _, row := range snapshot.Rows {
		fmt.Println(formatRow(row))
	}
	return 0
}

func formatRow(row tui.Row) string {
	parts := []string{row.Repo, row.Workflow, string(row.Status)}
	if row.Branch != "" {
		parts = append(parts, row.Branch)
	}
	if row.Title != "" {
		parts = append(parts, row.Title)
	}
	if row.Error != "" {
		parts = append(parts, row.Error)
	}
	return strings.Join(parts, "\t")
}
