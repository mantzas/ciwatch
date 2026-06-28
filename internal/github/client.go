package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const apiBase = "https://api.github.com"

type CommandContext func(context.Context, string, ...string) *exec.Cmd

type Client struct {
	token string
	http  *http.Client
	base  string
	ua    string
	sem   chan struct{}
}

type Rate struct {
	Limit     int
	Remaining int
	Reset     time.Time
}

type Run struct {
	ID           int64
	Attempt      int64
	Name         string
	Status       string
	Conclusion   string
	Branch       string
	Event        string
	Title        string
	HeadSHA      string
	URL          string
	CreatedAt    time.Time
	UpdatedAt    time.Time
	RunStartedAt time.Time
}

type RunsResult struct {
	Repo        string
	Runs        []Run
	ETag        string
	NotModified bool
	Rate        Rate
}

type APIError struct {
	StatusCode int
	Message    string
	Rate       Rate
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("github api %d: %s", e.StatusCode, e.Message)
	}
	return fmt.Sprintf("github api %d", e.StatusCode)
}

func NewClient(token string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	return &Client{token: token, http: httpClient, base: apiBase, ua: "ciwatch/dev", sem: make(chan struct{}, 4)}
}

func (c *Client) SetBaseURL(base string) {
	c.base = strings.TrimRight(base, "/")
}

func TokenFromGH(ctx context.Context, command CommandContext, timeout time.Duration) (string, error) {
	if command == nil {
		command = exec.CommandContext
	}
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := command(ctx, "gh", "auth", "token")
	out, err := cmd.Output()
	if err != nil {
		return "", errors.New("unable to read token from gh")
	}
	token := strings.TrimSpace(string(out))
	if token == "" {
		return "", errors.New("gh returned an empty token")
	}
	return token, nil
}

func (c *Client) WorkflowRuns(ctx context.Context, repo string, perPage int, etag string) (RunsResult, error) {
	c.sem <- struct{}{}
	defer func() { <-c.sem }()

	endpoint := fmt.Sprintf("%s/repos/%s/actions/runs?per_page=%d", c.base, repo, perPage)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return RunsResult{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", c.ua)
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return RunsResult{}, err
	}
	defer func() { _ = resp.Body.Close() }()

	result := RunsResult{Repo: repo, ETag: resp.Header.Get("ETag"), Rate: parseRate(resp.Header)}
	if resp.StatusCode == http.StatusNotModified {
		result.NotModified = true
		return result, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := readErrorMessage(resp.Body)
		return result, &APIError{StatusCode: resp.StatusCode, Message: msg, Rate: result.Rate}
	}

	var payload struct {
		WorkflowRuns []struct {
			ID           int64     `json:"id"`
			RunAttempt   int64     `json:"run_attempt"`
			Name         string    `json:"name"`
			Status       string    `json:"status"`
			Conclusion   string    `json:"conclusion"`
			HeadBranch   string    `json:"head_branch"`
			HeadSHA      string    `json:"head_sha"`
			Event        string    `json:"event"`
			DisplayTitle string    `json:"display_title"`
			HTMLURL      string    `json:"html_url"`
			CreatedAt    time.Time `json:"created_at"`
			UpdatedAt    time.Time `json:"updated_at"`
			RunStartedAt time.Time `json:"run_started_at"`
		} `json:"workflow_runs"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return result, err
	}
	for _, raw := range payload.WorkflowRuns {
		result.Runs = append(result.Runs, Run{
			ID: raw.ID, Attempt: raw.RunAttempt, Name: raw.Name, Status: raw.Status,
			Conclusion: raw.Conclusion, Branch: raw.HeadBranch, Event: raw.Event,
			Title: raw.DisplayTitle, HeadSHA: raw.HeadSHA, URL: raw.HTMLURL,
			CreatedAt: raw.CreatedAt, UpdatedAt: raw.UpdatedAt, RunStartedAt: raw.RunStartedAt,
		})
	}
	return result, nil
}

func parseRate(h http.Header) Rate {
	limit, _ := strconv.Atoi(h.Get("X-RateLimit-Limit"))
	remaining, _ := strconv.Atoi(h.Get("X-RateLimit-Remaining"))
	resetUnix, _ := strconv.ParseInt(h.Get("X-RateLimit-Reset"), 10, 64)
	var reset time.Time
	if resetUnix > 0 {
		reset = time.Unix(resetUnix, 0)
	}
	return Rate{Limit: limit, Remaining: remaining, Reset: reset}
}

func readErrorMessage(r io.Reader) string {
	var payload struct {
		Message string `json:"message"`
	}
	if err := json.NewDecoder(r).Decode(&payload); err == nil && payload.Message != "" {
		return payload.Message
	}
	return ""
}

func RepoURL(repo string) string {
	return (&url.URL{Scheme: "https", Host: "github.com", Path: "/" + repo}).String()
}
