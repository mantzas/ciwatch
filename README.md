# ciwatch

`ciwatch` is a terminal watcher for explicitly configured GitHub repositories. It monitors recent GitHub Actions workflow runs, shows a live Bubble Tea TUI, and notifies once when a newly observed completed run is broken.

The MVP supports GitHub Actions on `github.com` only. It does not discover repositories from users or organizations.

## Install

```sh
go install github.com/mantzas/ciwatch/cmd/ciwatch@latest
```

`ciwatch` uses the GitHub CLI for authentication:

```sh
gh auth login
```

At startup, `ciwatch` runs `gh auth token`, keeps the token in memory only, and never writes it to config, cache, logs, or errors.

## Config

Config lookup order:

1. `--config <path>`
2. `./config.toml`
3. `$XDG_CONFIG_HOME/ciwatch/config.toml` or `~/.config/ciwatch/config.toml`

Missing config prints the checked paths and a commented sample. Files are never created automatically.

```toml
repos = ["owner/repo"]

# poll_interval = "60s"
# runs_per_repo = 5
# notify_macos = true
```

Validation rejects malformed TOML, unknown fields, duplicate repos, invalid `owner/repo` values, `poll_interval` below `15s`, and `runs_per_repo` outside `1..20`.

## Usage

```sh
ciwatch
ciwatch --config ./config.toml
ciwatch --doctor
ciwatch --print-config-paths
ciwatch --once
ciwatch --version
```

Keys:

- `↑`/`↓` or `j`/`k`: navigate
- `r`: refresh now
- `o`: open the selected workflow run or repository
- `q` or `Ctrl+C`: quit

Rows are ordered as repository errors first, workflow runs by last run time descending, and repositories with no runs last.

For setup and scripts:

- `--doctor`: checks config loading, `gh auth token`, and the cache path.
- `--print-config-paths`: prints the config paths that would be checked.
- `--once`: fetches once, prints a tab-separated summary, and exits without the TUI.

## Notifications

Broken conclusions are `failure`, `timed_out`, and `action_required`. `cancelled` and `skipped` are neutral.

The first poll after a missing or rebuilt cache baselines the current state without notifications. Later broken run attempts are deduped by `repo`, run ID, and run attempt, so a failed rerun can notify again.

Terminal notifications appear in the TUI event log. Native desktop notifications are enabled by default where supported:

- macOS uses `osascript`.
- Linux uses `notify-send`, when available.
- Windows uses built-in PowerShell toast APIs on supported desktops.

The existing `notify_macos` setting controls native desktop notifications for compatibility and can be disabled with:

```toml
notify_macos = false
```

Notifications never open URLs automatically.

## Cache

State is stored as JSON at `$XDG_CACHE_HOME/ciwatch/state.json` or `~/.cache/ciwatch/state.json`. The cache stores only notification dedupe keys and per-repo ETags. It does not store tokens or run-history snapshots.

If the cache is missing or corrupt, `ciwatch` warns and rebuilds it with first-poll baseline behavior.

## Rate Limits

Each refresh calls the GitHub workflow-runs REST endpoint once per configured repository, with `per_page = runs_per_repo`. Requests use ETags and `If-None-Match`, so unchanged repositories can return `304 Not Modified` and reduce primary rate-limit usage.

The footer shows repository count, projected worst-case budget use, live remaining/reset values when available, and the next refresh. `ciwatch` warns when the configured cadence projects above 70% of the normal authenticated REST budget, but it does not change your polling interval automatically.

## Troubleshooting

- Start with `ciwatch --doctor` to check config, GitHub auth, and cache state.
- `github auth failed`: install `gh`, ensure it is on `PATH`, and run `gh auth login`.
- `config not found`: create a config at one of the checked paths or pass `--config`.
- API errors appear as repository-level `ERROR` rows and retry on the next poll.
- Native notification failures appear once in the TUI event log.
