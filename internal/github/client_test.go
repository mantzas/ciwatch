package github

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestWorkflowRunsSuccessETagAndRate(t *testing.T) {
	reset := time.Now().Add(time.Hour).Unix()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer token" {
			t.Fatalf("auth header = %q", got)
		}
		if got := r.Header.Get("If-None-Match"); got != `"old"` {
			t.Fatalf("etag header = %q", got)
		}
		w.Header().Set("ETag", `"new"`)
		w.Header().Set("X-RateLimit-Limit", "5000")
		w.Header().Set("X-RateLimit-Remaining", "4999")
		w.Header().Set("X-RateLimit-Reset", fmt.Sprint(reset))
		_, _ = fmt.Fprint(w, `{"workflow_runs":[{"id":1,"run_attempt":2,"name":"CI","status":"completed","conclusion":"failure","head_branch":"main","head_sha":"abcdef123","event":"pull_request","display_title":"fix","html_url":"https://github.com/a/b/actions/runs/1","created_at":"2026-06-28T10:00:00Z","updated_at":"2026-06-28T10:02:00Z","run_started_at":"2026-06-28T10:01:00Z","pull_requests":[{"number":12,"html_url":"https://github.com/a/b/pull/12"}]}]}`)
	}))
	defer srv.Close()
	client := NewClient("token", srv.Client())
	client.SetBaseURL(srv.URL)
	res, err := client.WorkflowRuns(context.Background(), "a/b", 5, `"old"`)
	if err != nil {
		t.Fatal(err)
	}
	if res.ETag != `"new"` || res.Rate.Limit != 5000 || len(res.Runs) != 1 || res.Runs[0].Attempt != 2 {
		t.Fatalf("unexpected result: %+v", res)
	}
	if len(res.Runs[0].PullRequests) != 1 || res.Runs[0].PullRequests[0].Number != 12 || res.Runs[0].PullRequests[0].URL != "https://github.com/a/b/pull/12" {
		t.Fatalf("pull request metadata not parsed: %+v", res.Runs[0].PullRequests)
	}
}

func TestWorkflowRunsNotModifiedAndAPIError(t *testing.T) {
	call := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call++
		if call == 2 {
			w.WriteHeader(http.StatusForbidden)
			_, _ = fmt.Fprint(w, `{"message":"rate limited"}`)
			return
		}
		w.WriteHeader(http.StatusNotModified)
	}))
	defer srv.Close()
	client := NewClient("token", srv.Client())
	client.SetBaseURL(srv.URL)
	res, err := client.WorkflowRuns(context.Background(), "a/b", 5, "")
	if err != nil || !res.NotModified {
		t.Fatalf("expected 304, got %+v %v", res, err)
	}
	_, err = client.WorkflowRuns(context.Background(), "a/b", 5, "")
	if err == nil {
		t.Fatal("expected api error")
	}
}

func TestTokenFromGH(t *testing.T) {
	token, err := TokenFromGH(context.Background(), func(ctx context.Context, name string, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "printf", "abc\n")
	}, time.Second)
	if err != nil || token != "abc" {
		t.Fatalf("token=%q err=%v", token, err)
	}
}

func TestClientDefaultsAPIErrorAndRepoURL(t *testing.T) {
	client := NewClient("token", nil)
	if client.http == nil || client.base != apiBase || cap(client.sem) != 4 {
		t.Fatalf("client defaults not applied: %+v", client)
	}
	if got := (&APIError{StatusCode: http.StatusForbidden}).Error(); got != "github api 403" {
		t.Fatalf("api error without message = %q", got)
	}
	if got := RepoURL("Owner/Repo With Space"); got != "https://github.com/Owner/Repo%20With%20Space" {
		t.Fatalf("RepoURL = %q", got)
	}
	if got := PullRequestURL("Owner/Repo", 42); got != "https://github.com/Owner/Repo/pull/42" {
		t.Fatalf("PullRequestURL = %q", got)
	}
}

func TestTokenFromGHErrors(t *testing.T) {
	_, err := TokenFromGH(context.Background(), func(ctx context.Context, name string, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "printf", " \n")
	}, time.Second)
	if err == nil || !strings.Contains(err.Error(), "empty token") {
		t.Fatalf("expected empty token error, got %v", err)
	}

	_, err = TokenFromGH(context.Background(), func(ctx context.Context, name string, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "false")
	}, 0)
	if err == nil || !strings.Contains(err.Error(), "unable to read token") {
		t.Fatalf("expected command error, got %v", err)
	}
}

func TestWorkflowRunsInvalidResponseBranches(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/bad-json/") {
			_, _ = fmt.Fprint(w, `{`)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprint(w, `not-json`)
	}))
	defer srv.Close()
	client := NewClient("token", srv.Client())
	client.SetBaseURL(srv.URL + "/bad-json")
	if _, err := client.WorkflowRuns(context.Background(), "a/b", 5, ""); err == nil {
		t.Fatal("expected bad json error")
	}

	client.SetBaseURL(srv.URL)
	_, err := client.WorkflowRuns(context.Background(), "a/b", 5, "")
	apiErr, ok := err.(*APIError)
	if !ok || apiErr.Message != "" || apiErr.Error() != "github api 500" {
		t.Fatalf("expected empty-message API error, got %#v", err)
	}

	if got := readErrorMessage(strings.NewReader(`{"message":"nope"}`)); got != "nope" {
		t.Fatalf("readErrorMessage json = %q", got)
	}
	if got := readErrorMessage(io.NopCloser(strings.NewReader(`not-json`))); got != "" {
		t.Fatalf("readErrorMessage invalid = %q", got)
	}
}
