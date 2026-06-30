## Purpose

Define the terminal UI behavior for making CI health, row status, focus, and operational context visible at a glance.

## Requirements

### Requirement: Status summary strip
The TUI SHALL show a compact status summary above the workflow table that counts the currently loaded rows by actionable status category.

#### Scenario: Rows are loaded
- **WHEN** the TUI has rows with broken, running, ok, neutral/no-run, and error statuses
- **THEN** the view displays a summary containing counts for broken, running, ok, quiet/no-run, and error categories

#### Scenario: No rows are loaded
- **WHEN** the TUI has no rows yet
- **THEN** the view still renders a stable summary area without panicking or hiding the table controls

### Requirement: Semantic status presentation
The TUI SHALL render each status with explicit text and semantic color so status remains understandable without relying on color alone.

#### Scenario: Broken workflow row
- **WHEN** a workflow row has broken status
- **THEN** the status cell displays an explicit broken label and uses urgent styling

#### Scenario: Running workflow row
- **WHEN** a workflow row has running status
- **THEN** the status cell displays an explicit running label and uses active/in-progress styling

#### Scenario: Successful workflow row
- **WHEN** a workflow row has ok status
- **THEN** the status cell displays an explicit ok label and uses success styling

#### Scenario: Neutral or no-run row
- **WHEN** a row is neutral or represents no workflow runs
- **THEN** the status cell displays non-urgent user-facing language and uses muted styling

#### Scenario: Repository error row
- **WHEN** a row represents a repository fetch error
- **THEN** the status cell displays an explicit error label and uses urgent styling distinct enough to scan

### Requirement: Focus and action clarity
The TUI SHALL make the selected row visually distinct and keep the available keyboard actions visible.

#### Scenario: Row is selected
- **WHEN** the user navigates to a row
- **THEN** the selected row is visibly distinct from surrounding rows

#### Scenario: Footer is rendered
- **WHEN** the TUI view is rendered
- **THEN** the footer continues to show navigation, refresh, open, and quit controls

### Requirement: Operational context styling
The TUI SHALL visually distinguish rate-risk, refresh, and recent event states while preserving existing behavior.

#### Scenario: Rate risk is present
- **WHEN** the rate status indicates warning
- **THEN** the header displays rate risk with warning styling

#### Scenario: Refresh is in progress
- **WHEN** the model is refreshing
- **THEN** the header displays refreshing state with active styling

#### Scenario: Recent events are displayed
- **WHEN** recent events are present
- **THEN** urgent events are visually emphasized and non-urgent events remain readable without overwhelming the table

### Requirement: Existing behavior preservation
The TUI visual update SHALL NOT change CI data fetching, row ordering, notification dedupe, cache persistence, command-line flags, or keyboard command behavior.

#### Scenario: Keyboard commands are used
- **WHEN** the user navigates, refreshes, opens, or quits using existing key bindings
- **THEN** those commands behave as they did before the visual update

#### Scenario: Once mode is used
- **WHEN** `ciwatch --once` is run
- **THEN** the tab-separated once output remains unchanged by the TUI visual update
