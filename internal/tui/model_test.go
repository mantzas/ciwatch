package tui

import (
	"context"
	"errors"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mantzas/ciwatch/internal/config"
	ghapi "github.com/mantzas/ciwatch/internal/github"
	"github.com/mantzas/ciwatch/internal/notify"
	"github.com/mantzas/ciwatch/internal/state"
)

func TestClassifyAndSortRows(t *testing.T) {
	if Classify("completed", "failure") != StatusBroken || Classify("completed", "cancelled") != StatusNeutral || Classify("in_progress", "") != StatusRunning {
		t.Fatal("bad classification")
	}
	now := time.Now()
	rows := []Row{
		{Kind: RowNoRuns, Repo: "z"},
		{Kind: RowRun, Repo: "b", Workflow: "old", UpdatedAt: now.Add(-time.Hour)},
		{Kind: RowError, Repo: "a"},
		{Kind: RowRun, Repo: "a", Workflow: "new", UpdatedAt: now},
	}
	SortRows(rows)
	if rows[0].Kind != RowError || rows[1].Workflow != "new" || rows[3].Kind != RowNoRuns {
		t.Fatalf("bad order: %+v", rows)
	}
}

func TestRunnerBaselineETagAndNotifyDedupe(t *testing.T) {
	cfg := config.Config{Repos: []string{"A/B"}, PollInterval: time.Minute, RunsPerRepo: 5, NotifyMacOS: true}
	client := &fakeClient{runs: []ghapi.Run{{ID: 1, Attempt: 1, Name: "CI", Status: "completed", Conclusion: "failure", Branch: "main", UpdatedAt: time.Now(), URL: "u"}}}
	runner := NewRunner(cfg, client, state.New(), true)
	first, err := runner.Refresh(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Events) != 0 || first.Rows[0].Notify {
		t.Fatalf("baseline should not notify: %+v", first)
	}
	second, err := runner.Refresh(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Events) != 0 {
		t.Fatalf("dedupe failed: %+v", second.Events)
	}
	client.runs[0].Attempt = 2
	third, err := runner.Refresh(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(third.Events) != 1 || !third.Rows[0].Notify {
		t.Fatalf("rerun attempt should notify: %+v", third)
	}
}

func TestModelKeyHandlingOpenAndEventCap(t *testing.T) {
	cfg := config.Config{Repos: []string{"a/b"}, PollInterval: time.Minute, RunsPerRepo: 1}
	runner := &fakeRunner{snapshot: Snapshot{Rows: []Row{{Kind: RowRun, Repo: "a/b", URL: "https://example.test"}}}}
	notifier := &fakeNotifier{}
	model := NewModel(cfg, runner, notifier, "state.json")
	model.rows = runner.snapshot.Rows
	model.applyRows()
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")})
	got := updated.(Model)
	if notifier.opened != "https://example.test" {
		t.Fatalf("open not called: %q", notifier.opened)
	}
	for i := 0; i < 25; i++ {
		got.addEvent("x")
	}
	if len(got.events) != 20 {
		t.Fatalf("event cap failed: %d", len(got.events))
	}
}

type fakeClient struct {
	runs []ghapi.Run
	err  error
}

func (f *fakeClient) WorkflowRuns(context.Context, string, int, string) (ghapi.RunsResult, error) {
	if f.err != nil {
		return ghapi.RunsResult{}, f.err
	}
	return ghapi.RunsResult{Runs: f.runs, ETag: `"e"`, Rate: ghapi.Rate{Limit: 5000, Remaining: 4999}}, nil
}

type fakeRunner struct {
	snapshot Snapshot
	err      error
	cache    state.Cache
}

func (f *fakeRunner) Refresh(context.Context) (Snapshot, error) {
	if f.err != nil {
		return Snapshot{}, f.err
	}
	return f.snapshot, nil
}

func (f *fakeRunner) Cache() state.Cache {
	return f.cache
}

type fakeNotifier struct {
	opened string
	err    error
}

func (f *fakeNotifier) MacOS(bool, notify.Notification) error {
	return errors.New("unused")
}

func (f *fakeNotifier) Open(url string) error {
	f.opened = url
	return f.err
}
