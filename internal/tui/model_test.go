package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
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

func TestRunnerEndToEndRowsCacheErrorsAndNotModified(t *testing.T) {
	now := time.Now()
	cfg := config.Config{
		Repos:        []string{"owner/failing", "owner/empty", "owner/error"},
		PollInterval: time.Minute,
		RunsPerRepo:  5,
		NotifyMacOS:  true,
	}
	client := &repoClient{
		results: map[string]ghapi.RunsResult{
			"owner/failing": {
				Runs: []ghapi.Run{{
					ID: 10, Attempt: 1, Name: "CI", Status: "completed", Conclusion: "failure",
					Branch: "main", Event: "push", Title: "break build", URL: "https://example.test/run",
					UpdatedAt: now, RunStartedAt: now.Add(-time.Minute),
				}},
				ETag: `"fail-etag"`,
				Rate: ghapi.Rate{Limit: 5000, Remaining: 4990, Reset: now.Add(time.Hour)},
			},
			"owner/empty": {ETag: `"empty-etag"`, Rate: ghapi.Rate{Limit: 5000, Remaining: 4980, Reset: now.Add(2 * time.Hour)}},
		},
		errs: map[string]error{
			"owner/error": &ghapi.APIError{StatusCode: 500, Message: "server down", Rate: ghapi.Rate{Limit: 5000, Remaining: 4970, Reset: now.Add(30 * time.Minute)}},
		},
	}
	runner := NewRunner(cfg, client, state.New(), true)

	first, err := runner.Refresh(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Events) != 0 {
		t.Fatalf("baseline should not emit events: %+v", first.Events)
	}
	assertRow(t, first.Rows, "owner/error", RowError, StatusError, false)
	assertRow(t, first.Rows, "owner/failing", RowRun, StatusBroken, false)
	assertRow(t, first.Rows, "owner/empty", RowNoRuns, StatusNeutral, false)
	if !runner.Cache().WasNotified("owner/failing", 10, 1) {
		t.Fatal("baseline should mark the failed run as seen")
	}
	if runner.Cache().ETags["owner/failing"] != `"fail-etag"` || runner.Cache().ETags["owner/empty"] != `"empty-etag"` {
		t.Fatalf("missing etags: %+v", runner.Cache().ETags)
	}
	if first.Rate.Remaining != 4970 || first.Rate.Reset != now.Add(30*time.Minute) || first.Rate.Projected == 0 {
		t.Fatalf("rate was not merged conservatively: %+v", first.Rate)
	}

	client.results["owner/failing"] = ghapi.RunsResult{Repo: "owner/failing", NotModified: true, Rate: ghapi.Rate{Limit: 5000, Remaining: 4960, Reset: now.Add(time.Hour)}}
	client.results["owner/empty"] = ghapi.RunsResult{Repo: "owner/empty", NotModified: true}
	delete(client.errs, "owner/error")
	client.results["owner/error"] = ghapi.RunsResult{Runs: []ghapi.Run{{
		ID: 11, Attempt: 1, Name: "CI", Status: "completed", Conclusion: "failure",
		Branch: "main", Event: "push", Title: "new failure", URL: "https://example.test/run/11",
		UpdatedAt: now.Add(time.Minute),
	}}}
	second, err := runner.Refresh(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	assertRow(t, second.Rows, "owner/failing", RowRun, StatusBroken, false)
	assertRow(t, second.Rows, "owner/empty", RowNoRuns, StatusNeutral, false)
	assertRow(t, second.Rows, "owner/error", RowRun, StatusBroken, true)
	if len(second.Events) != 1 || !strings.Contains(second.Events[0], "owner/error CI main") {
		t.Fatalf("expected one new failure event: %+v", second.Events)
	}
}

func TestModelRefreshMessageSendsOneNotificationFailureEvent(t *testing.T) {
	cfg := config.Config{Repos: []string{"a/b"}, PollInterval: time.Minute, RunsPerRepo: 1, NotifyMacOS: true}
	notifier := &fakeNotifier{notifyErr: errors.New("blocked")}
	model := NewModel(cfg, &fakeRunner{cache: state.New()}, notifier, "state.json")
	row := Row{Kind: RowRun, Repo: "a/b", Workflow: "CI", Status: StatusBroken, Branch: "main", Title: "failed", Notify: true}

	updated, _ := model.Update(refreshMsg{snapshot: Snapshot{Rows: []Row{row, row}, Events: []string{"broken: a/b CI main"}}})
	got := updated.(Model)
	if notifier.notifications != 2 {
		t.Fatalf("notifications = %d", notifier.notifications)
	}
	if len(got.events) != 2 || !strings.Contains(got.events[1], "macOS notification failed: blocked") {
		t.Fatalf("events = %+v", got.events)
	}

	updated, _ = got.Update(refreshMsg{snapshot: Snapshot{Rows: []Row{row}}})
	got = updated.(Model)
	if notifier.notifications != 3 {
		t.Fatalf("notifications after second refresh = %d", notifier.notifications)
	}
	failures := 0
	for _, event := range got.events {
		if strings.Contains(event, "macOS notification failed") {
			failures++
		}
	}
	if failures != 1 {
		t.Fatalf("notification failure should be reported once: %+v", got.events)
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

func TestModelViewSaveRefreshAndResize(t *testing.T) {
	cfg := config.Config{Repos: []string{"a/b", "c/d"}, PollInterval: time.Minute, RunsPerRepo: 1}
	cachePath := filepath.Join(t.TempDir(), "state.json")
	cache := state.New()
	cache.Baseline = true
	model := NewModel(cfg, &fakeRunner{cache: cache}, &fakeNotifier{}, cachePath)
	model.rows = []Row{{
		Kind: RowRun, Repo: "a/b", Workflow: "CI", Status: StatusOK, Branch: "main",
		Event: "push", Title: "ok", UpdatedAt: time.Now().Add(-time.Minute),
		StartedAt: time.Now().Add(-2 * time.Minute), FinishedAt: time.Now().Add(-time.Minute),
	}}
	model.rate = RateStatus{Limit: 5000, Remaining: 4990, Reset: time.Now().Add(time.Hour), Projected: 0.25, Warning: true}
	model.err = "last refresh failed"
	model.refreshing = true
	model.next = time.Now().Add(time.Minute)
	model.addEvent("broken: a/b CI main")
	model.applyRows()

	view := model.View()
	for _, want := range []string{"repos:2", "RATE RISK", "refreshing", "last refresh failed", "broken: a/b CI main"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
	if err := model.SaveState(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(cachePath); err != nil {
		t.Fatalf("state was not saved: %v", err)
	}

	updated, _ := model.Update(tea.WindowSizeMsg{Width: 96, Height: 18})
	got := updated.(Model)
	if got.width != 96 || got.height != 18 || len(got.table.Columns()) != 8 {
		t.Fatalf("resize not applied: width=%d height=%d cols=%d", got.width, got.height, len(got.table.Columns()))
	}
}

func TestModelRefreshCommandPendingAndError(t *testing.T) {
	cfg := config.Config{Repos: []string{"a/b"}, PollInterval: time.Minute, RunsPerRepo: 1}
	runner := &fakeRunner{snapshot: Snapshot{Rows: []Row{{Kind: RowRun, Repo: "a/b", Workflow: "CI", Status: StatusOK}}}, cache: state.New()}
	model := NewModel(cfg, runner, &fakeNotifier{}, "state.json")

	queued, cmd := model.queueRefresh()
	if !queued.refreshing || cmd == nil {
		t.Fatalf("refresh was not queued: refreshing=%v cmd nil=%v", queued.refreshing, cmd == nil)
	}
	queuedAgain, cmd := queued.queueRefresh()
	if !queuedAgain.pending || cmd != nil {
		t.Fatalf("second refresh should mark pending without command: pending=%v cmd=%v", queuedAgain.pending, cmd)
	}

	msg := runnerRefreshMsg(t, queued.refreshCmd())
	if msg.err != nil || len(msg.snapshot.Rows) != 1 {
		t.Fatalf("refresh command returned %#v", msg)
	}
	updated, cmd := queuedAgain.Update(msg)
	got := updated.(Model)
	if !got.refreshing || got.pending || cmd == nil {
		t.Fatalf("pending refresh was not started: refreshing=%v pending=%v cmd nil=%v", got.refreshing, got.pending, cmd == nil)
	}

	runner.err = errors.New("network down")
	msg = runnerRefreshMsg(t, got.refreshCmd())
	updated, _ = got.Update(msg)
	got = updated.(Model)
	if got.err != "network down" || len(got.events) == 0 || !strings.Contains(got.events[len(got.events)-1], "refresh failed: network down") {
		t.Fatalf("refresh error not recorded: err=%q events=%+v", got.err, got.events)
	}
}

func TestModelTickOpenErrorAndQuitKeys(t *testing.T) {
	cfg := config.Config{Repos: []string{"a/b"}, PollInterval: time.Minute, RunsPerRepo: 1}
	model := NewModel(cfg, &fakeRunner{cache: state.New()}, &fakeNotifier{err: errors.New("no browser")}, "state.json")
	model.next = time.Now().Add(-time.Second)
	updated, cmd := model.Update(tickMsg(time.Now()))
	got := updated.(Model)
	if !got.refreshing || cmd == nil {
		t.Fatalf("expired tick should queue refresh: refreshing=%v cmd nil=%v", got.refreshing, cmd == nil)
	}

	model.rows = []Row{{Kind: RowRun, Repo: "a/b", URL: "https://example.test"}}
	model.applyRows()
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")})
	got = updated.(Model)
	if len(got.events) == 0 || !strings.Contains(got.events[len(got.events)-1], "open failed: no browser") {
		t.Fatalf("open error not recorded: %+v", got.events)
	}
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if cmd == nil {
		t.Fatal("quit key should return a command")
	}
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("ctrl+c should return a command")
	}
}

func TestFormattingHelpers(t *testing.T) {
	now := time.Now()
	tests := map[Status]string{
		StatusBroken:  "✖ BROKEN",
		StatusRunning: "… RUNNING",
		StatusOK:      "✓ OK",
		StatusNeutral: "• NEUTRAL",
		StatusError:   "! ERROR",
	}
	for status, want := range tests {
		if got := statusLabel(status); got != want {
			t.Fatalf("statusLabel(%s) = %q, want %q", status, got, want)
		}
	}
	if displayRef(Row{Branch: "main", SHA: "abcdef123"}) != "main" || displayRef(Row{SHA: "abcdef123"}) != "abcdef123" {
		t.Fatal("displayRef did not prefer branch then SHA")
	}
	if titleSHA(Row{Title: "title", SHA: "abcdef123"}) != "title" || titleSHA(Row{SHA: "abcdef123"}) != "abcdef1" || titleSHA(Row{SHA: "abc"}) != "abc" {
		t.Fatal("titleSHA formatting failed")
	}
	if age(time.Time{}) != "-" || duration(time.Time{}, now) != "-" || until(time.Time{}) != "-" {
		t.Fatal("zero time helpers should return dashes")
	}
	if got := duration(now.Add(-90*time.Second), now); got != "1m30s" {
		t.Fatalf("duration = %q", got)
	}
	if min(1, 2) != 1 || max(1, 2) != 2 {
		t.Fatal("min/max failed")
	}
	model := NewModel(config.Config{}, &fakeRunner{cache: state.New()}, &fakeNotifier{}, "")
	if model.rateText() != "unknown" {
		t.Fatalf("empty rateText = %q", model.rateText())
	}
	model.rate = RateStatus{Limit: 5000, Remaining: 2500, Projected: 0.5}
	if got := model.rateText(); !strings.Contains(got, "2500/5000") || !strings.Contains(got, "projected:50%") {
		t.Fatalf("rateText = %q", got)
	}
	if NormalizeRepo("Owner/Repo") != "owner/repo" {
		t.Fatal("NormalizeRepo did not lowercase")
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
	opened        string
	err           error
	notifyErr     error
	notifications int
}

func (f *fakeNotifier) MacOS(bool, notify.Notification) error {
	f.notifications++
	return f.notifyErr
}

func (f *fakeNotifier) Open(url string) error {
	f.opened = url
	return f.err
}

type repoClient struct {
	results map[string]ghapi.RunsResult
	errs    map[string]error
}

func (f *repoClient) WorkflowRuns(_ context.Context, repo string, perPage int, etag string) (ghapi.RunsResult, error) {
	if perPage != 5 {
		return ghapi.RunsResult{}, errors.New("unexpected perPage")
	}
	if repo == "owner/failing" && etag != "" && etag != `"fail-etag"` {
		return ghapi.RunsResult{}, errors.New("unexpected etag")
	}
	if err := f.errs[repo]; err != nil {
		return ghapi.RunsResult{}, err
	}
	res := f.results[repo]
	res.Repo = repo
	return res, nil
}

func assertRow(t *testing.T, rows []Row, repo string, kind RowKind, status Status, notify bool) {
	t.Helper()
	for _, row := range rows {
		if row.Repo == repo {
			if row.Kind != kind || row.Status != status || row.Notify != notify {
				t.Fatalf("row %s = %+v, want kind=%v status=%s notify=%v", repo, row, kind, status, notify)
			}
			return
		}
	}
	t.Fatalf("row for %s not found in %+v", repo, rows)
}

func runnerRefreshMsg(t *testing.T, cmd tea.Cmd) refreshMsg {
	t.Helper()
	msg, ok := cmd().(refreshMsg)
	if !ok {
		t.Fatalf("command returned %#v", msg)
	}
	return msg
}
