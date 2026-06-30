## 1. Rendering Helpers

- [x] 1.1 Add a helper that derives status summary counts from the current TUI rows.
- [x] 1.2 Add focused helpers for summary text, status labels, event severity styling, and header state styling.
- [x] 1.3 Keep helper behavior deterministic and testable without requiring a live terminal.

## 2. TUI Presentation

- [x] 2.1 Render the status summary strip above the workflow table.
- [x] 2.2 Apply semantic status styling for broken, running, ok, quiet/no-run, and error states.
- [x] 2.3 Improve selected-row, table header, and grouped row styling using existing Bubbles table and Lip Gloss APIs.
- [x] 2.4 Style rate-risk, refreshing, and recent event states while preserving existing text cues.
- [x] 2.5 Preserve existing keyboard controls, row ordering, refresh behavior, open behavior, notifications, cache behavior, and `--once` output.

## 3. Verification

- [x] 3.1 Add or update TUI tests for summary counts, user-facing status labels, and preserved footer controls.
- [x] 3.2 Run `go test ./...`.
- [x] 3.3 Run `go vet ./...`.
- [x] 3.4 Run `golangci-lint run ./...`.
