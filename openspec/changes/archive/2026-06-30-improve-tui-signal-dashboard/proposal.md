## Why

`ciwatch` already surfaces the right CI data, but the current terminal UI is visually flat and makes the user scan rows to understand overall health. A more vibrant, status-first interface can make broken workflows, running work, quiet repos, rate risk, and recent events obvious at a glance without changing the core watcher behavior.

## What Changes

- Add a compact status summary strip above the workflow table with counts for broken, running, ok, quiet/no-run, and error rows.
- Improve the table's visual hierarchy using a consistent, vivid color system for statuses, selection, grouped repository/context cells, rate-risk state, refresh state, and recent events.
- Rename neutral/no-run presentation in the UI to friendlier user-facing language such as `QUIET` or `NO RUNS` while preserving existing internal status semantics.
- Make the selected row and actionable `open` target easier to identify.
- Keep the existing keyboard controls, row ordering, GitHub data fetching, notification behavior, and cache behavior unchanged.

## Capabilities

### New Capabilities

- `tui-signal-dashboard`: Covers the terminal UI's status summary, visual hierarchy, color semantics, selected-row affordance, and event presentation.

### Modified Capabilities

- None.

## Impact

- Affected code: primarily `internal/tui/model.go`, with focused updates to view rendering, table styles, row display text, and status/event formatting.
- Affected tests: TUI model/view tests may need updates or additions to cover the summary strip, user-facing status labels, and preserved keyboard behavior.
- Dependencies: no new runtime dependency is expected; the existing Bubble Tea, Bubbles table, and Lip Gloss stack is sufficient.
- Compatibility: no config, CLI flag, cache format, GitHub API, notification, or `--once` output behavior should change.
