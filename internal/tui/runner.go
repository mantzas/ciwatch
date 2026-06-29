package tui

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/mantzas/ciwatch/internal/config"
	ghapi "github.com/mantzas/ciwatch/internal/github"
	"github.com/mantzas/ciwatch/internal/state"
)

type GitHubClient interface {
	WorkflowRuns(context.Context, string, int, string) (ghapi.RunsResult, error)
}

type GitHubRunner struct {
	cfg       config.Config
	client    GitHubClient
	cache     state.Cache
	baseline  bool
	lastRows  map[string][]Row
	lastError map[string]string
}

func NewRunner(cfg config.Config, client GitHubClient, cache state.Cache, rebuilt bool) *GitHubRunner {
	if cache.SchemaVersion == 0 {
		cache.SchemaVersion = state.SchemaVersion
	}
	return &GitHubRunner{
		cfg: cfg, client: client, cache: cache, baseline: rebuilt || !cache.Baseline,
		lastRows: map[string][]Row{}, lastError: map[string]string{},
	}
}

func (r *GitHubRunner) Refresh(ctx context.Context) (Snapshot, error) {
	type result struct {
		repo string
		res  ghapi.RunsResult
		err  error
	}
	ch := make(chan result, len(r.cfg.Repos))
	var wg sync.WaitGroup
	for _, repo := range r.cfg.Repos {
		repo := repo
		wg.Add(1)
		go func() {
			defer wg.Done()
			key := state.RepoKey(repo)
			res, err := r.client.WorkflowRuns(ctx, repo, r.cfg.RunsPerRepo, r.cache.ETags[key])
			if err == nil && res.NotModified && len(r.lastRows[key]) == 0 {
				res, err = r.client.WorkflowRuns(ctx, repo, r.cfg.RunsPerRepo, "")
			}
			ch <- result{repo: repo, res: res, err: err}
		}()
	}
	wg.Wait()
	close(ch)

	var rows []Row
	var events []string
	rate := RateStatus{}
	for item := range ch {
		key := state.RepoKey(item.repo)
		if item.err != nil {
			r.lastError[key] = item.err.Error()
			rows = append(rows, Row{Kind: RowError, Repo: item.repo, Workflow: "-", Status: StatusError, URL: ghapi.RepoURL(item.repo), Error: item.err.Error()})
			if apiErr, ok := item.err.(*ghapi.APIError); ok {
				mergeRate(&rate, apiErr.Rate)
			}
			continue
		}
		delete(r.lastError, key)
		if item.res.ETag != "" {
			r.cache.ETags[key] = item.res.ETag
		}
		mergeRate(&rate, item.res.Rate)
		if item.res.NotModified {
			rows = append(rows, r.lastRows[key]...)
			continue
		}
		mapped := mapRuns(item.repo, item.res.Runs)
		if len(mapped) == 0 {
			mapped = []Row{{Kind: RowNoRuns, Repo: item.repo, Workflow: "-", Status: StatusNeutral, URL: ghapi.RepoURL(item.repo), Title: "no workflow runs"}}
		}
		for idx, row := range mapped {
			if row.Status != StatusBroken || r.cache.WasNotified(row.Repo, row.RunID, row.Attempt) {
				continue
			}
			if !r.baseline {
				mapped[idx].Notify = true
				events = append(events, fmt.Sprintf("broken: %s %s %s", row.Repo, row.Workflow, row.Branch))
			}
			r.cache.MarkNotified(row.Repo, row.RunID, row.Attempt)
		}
		r.lastRows[key] = mapped
		rows = append(rows, mapped...)
	}
	r.baseline = false
	r.cache.Baseline = true
	rate.Projected = projectedUsage(len(r.cfg.Repos), r.cfg.PollInterval)
	rate.Warning = rate.Projected > 0.70
	SortRowsByRepoOrder(rows, r.cfg.Repos)
	return Snapshot{Rows: rows, Rate: rate, Events: events}, nil
}

func (r *GitHubRunner) Cache() state.Cache {
	return r.cache
}

func mapRuns(repo string, runs []ghapi.Run) []Row {
	rows := make([]Row, 0, len(runs))
	for _, run := range runs {
		finished := run.UpdatedAt
		if run.Status != "completed" {
			finished = time.Time{}
		}
		rows = append(rows, Row{
			Kind: RowRun, Repo: repo, Workflow: run.Name, Status: Classify(run.Status, run.Conclusion),
			Context: runContext(repo, run), ContextKey: runContextKey(run), ContextURL: runContextURL(repo, run),
			Branch: run.Branch, Event: run.Event, Title: run.Title, SHA: run.HeadSHA, URL: run.URL,
			UpdatedAt: run.UpdatedAt, StartedAt: run.RunStartedAt, FinishedAt: finished,
			RunID: run.ID, Attempt: run.Attempt,
		})
	}
	return rows
}

func runContext(repo string, run ghapi.Run) string {
	if len(run.PullRequests) > 0 && run.PullRequests[0].Number > 0 {
		label := fmt.Sprintf("PR #%d", run.PullRequests[0].Number)
		if run.Title != "" {
			label += " " + run.Title
		}
		return label
	}
	ref := run.Branch
	if ref == "" {
		ref = run.HeadSHA
	}
	if run.Event == "push" {
		return ref + " direct push"
	}
	if run.Event != "" {
		return ref + " " + run.Event
	}
	return ref
}

func runContextKey(run ghapi.Run) string {
	if len(run.PullRequests) > 0 && run.PullRequests[0].Number > 0 {
		return fmt.Sprintf("pr:%d", run.PullRequests[0].Number)
	}
	if run.Event == "push" {
		return "push:" + run.Branch + ":" + run.HeadSHA
	}
	return run.Event + ":" + run.Branch + ":" + run.HeadSHA
}

func runContextURL(repo string, run ghapi.Run) string {
	if len(run.PullRequests) == 0 || run.PullRequests[0].Number == 0 {
		return ""
	}
	if run.PullRequests[0].URL != "" {
		return run.PullRequests[0].URL
	}
	return ghapi.PullRequestURL(repo, run.PullRequests[0].Number)
}

func mergeRate(target *RateStatus, rate ghapi.Rate) {
	if rate.Limit == 0 {
		return
	}
	target.Limit = rate.Limit
	if target.Remaining == 0 || rate.Remaining < target.Remaining {
		target.Remaining = rate.Remaining
	}
	if target.Reset.IsZero() || rate.Reset.Before(target.Reset) {
		target.Reset = rate.Reset
	}
}

func projectedUsage(repoCount int, interval time.Duration) float64 {
	if repoCount == 0 || interval <= 0 {
		return 0
	}
	perHour := float64(time.Hour) / float64(interval)
	return (float64(repoCount) * perHour) / 5000
}

func NormalizeRepo(repo string) string {
	return strings.ToLower(repo)
}
