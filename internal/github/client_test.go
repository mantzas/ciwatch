package github

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os/exec"
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
		_, _ = fmt.Fprint(w, `{"workflow_runs":[{"id":1,"run_attempt":2,"name":"CI","status":"completed","conclusion":"failure","head_branch":"main","head_sha":"abcdef123","event":"push","display_title":"fix","html_url":"https://github.com/a/b/actions/runs/1","created_at":"2026-06-28T10:00:00Z","updated_at":"2026-06-28T10:02:00Z","run_started_at":"2026-06-28T10:01:00Z"}]}`)
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
