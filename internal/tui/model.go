package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mantzas/ciwatch/internal/config"
	"github.com/mantzas/ciwatch/internal/notify"
	"github.com/mantzas/ciwatch/internal/state"
)

type Status string

const (
	StatusBroken  Status = "BROKEN"
	StatusRunning Status = "RUNNING"
	StatusOK      Status = "OK"
	StatusNeutral Status = "NEUTRAL"
	StatusError   Status = "ERROR"
)

type RowKind int

const (
	RowRun RowKind = iota
	RowError
	RowNoRuns
)

type Row struct {
	Kind       RowKind
	Repo       string
	Context    string
	ContextKey string
	ContextURL string
	Workflow   string
	Status     Status
	Branch     string
	Event      string
	Title      string
	SHA        string
	URL        string
	UpdatedAt  time.Time
	StartedAt  time.Time
	FinishedAt time.Time
	Error      string
	RunID      int64
	Attempt    int64
	Notify     bool
}

type RateStatus struct {
	Limit     int
	Remaining int
	Reset     time.Time
	Projected float64
	Warning   bool
}

type Snapshot struct {
	Rows   []Row
	Rate   RateStatus
	Events []string
}

type Runner interface {
	Refresh(context.Context) (Snapshot, error)
	Cache() state.Cache
}

type Notifier interface {
	MacOS(bool, notify.Notification) error
	Open(string) error
}

type Model struct {
	cfg               config.Config
	runner            Runner
	notifier          Notifier
	cachePath         string
	table             table.Model
	rows              []Row
	events            []string
	rate              RateStatus
	err               string
	width             int
	height            int
	next              time.Time
	refreshing        bool
	pending           bool
	notifiedOSFailure bool
}

type refreshMsg struct {
	snapshot Snapshot
	err      error
}

type tickMsg time.Time

func NewModel(cfg config.Config, runner Runner, notifier Notifier, cachePath string) Model {
	cols := []table.Column{
		{Title: "REPO", Width: 18}, {Title: "CONTEXT", Width: 28}, {Title: "WORKFLOW", Width: 20},
		{Title: "STATUS", Width: 12}, {Title: "REF", Width: 14},
		{Title: "DURATION", Width: 9}, {Title: "TITLE/SHA", Width: 28},
	}
	t := table.New(table.WithColumns(cols), table.WithFocused(true), table.WithHeight(12))
	t.SetStyles(tableStyles())
	return Model{cfg: cfg, runner: runner, notifier: notifier, cachePath: cachePath, table: t, next: time.Now()}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.refreshCmd(), tick())
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.table.SetHeight(max(5, msg.Height-7))
		m.resizeColumns(msg.Width)
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "r":
			return m.queueRefresh()
		case "o":
			if err := m.openSelected(); err != nil {
				m.addEvent("open failed: " + err.Error())
			}
		}
	case refreshMsg:
		m.refreshing = false
		m.next = time.Now().Add(m.cfg.PollInterval)
		if msg.err != nil {
			m.err = msg.err.Error()
			m.addEvent("refresh failed: " + msg.err.Error())
		} else {
			m.err = ""
			m.rows = msg.snapshot.Rows
			m.rate = msg.snapshot.Rate
			for _, event := range msg.snapshot.Events {
				m.addEvent(event)
			}
			m.applyRows()
			m.sendNotifications(msg.snapshot.Rows)
		}
		if m.pending {
			m.pending = false
			return m.queueRefresh()
		}
	case tickMsg:
		if time.Now().After(m.next) {
			next, cmd := m.queueRefresh()
			m = next
			return m, tea.Batch(cmd, tick())
		}
		return m, tick()
	}
	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m Model) View() string {
	var b strings.Builder
	b.WriteString(m.headerLine())
	b.WriteString("\n")
	b.WriteString(renderStatusSummary(statusSummaryForRows(m.rows)))
	b.WriteString("\n")
	if m.err != "" {
		b.WriteString(errorStyle().Render(m.err))
		b.WriteString("\n")
	}
	b.WriteString(m.renderTable())
	b.WriteString("\n")
	if len(m.events) > 0 {
		for _, event := range m.events[max(0, len(m.events)-3):] {
			b.WriteString(styledEvent(event))
			b.WriteString("\n")
		}
	} else {
		b.WriteString("\n")
	}
	b.WriteString(helpStyle().Render("↑/↓ jk navigate  r refresh  o open  q quit"))
	return b.String()
}

func (m Model) headerLine() string {
	parts := []string{
		titleStyle().Render("ciwatch"),
		fmt.Sprintf("repos:%d", len(m.cfg.Repos)),
		fmt.Sprintf("rate:%s", m.rateText()),
		fmt.Sprintf("next:%s", until(m.next)),
	}
	if m.rate.Warning {
		parts = append(parts, warningStyle().Render("RATE RISK"))
	}
	if m.refreshing {
		parts = append(parts, activeStyle().Render("refreshing"))
	}
	return strings.Join(parts, "  ")
}

func (m Model) renderTable() string {
	columns := m.table.Columns()
	rows := m.table.Rows()
	var b strings.Builder
	b.WriteString(renderTableHeader(columns))
	b.WriteString("\n")
	b.WriteString(strings.Repeat("─", tableWidth(columns)))

	height := max(1, m.table.Height())
	start := 0
	cursor := m.table.Cursor()
	if cursor >= height {
		start = cursor - height + 1
	}
	end := min(len(rows), start+height)
	for idx := start; idx < end; idx++ {
		b.WriteString("\n")
		line := renderTableRow(columns, rows[idx])
		if idx == cursor {
			line = selectedRowStyle().Render(fitCell(line, tableWidth(columns)))
		}
		b.WriteString(line)
	}
	return b.String()
}

func renderTableHeader(columns []table.Column) string {
	cells := make([]string, 0, len(columns))
	for _, col := range columns {
		cells = append(cells, headerCellStyle().Render(fitCell(col.Title, col.Width)))
	}
	return strings.Join(cells, "  ")
}

func renderTableRow(columns []table.Column, row table.Row) string {
	cells := make([]string, 0, len(columns))
	for idx, col := range columns {
		value := ""
		if idx < len(row) {
			value = row[idx]
		}
		cell := fitCell(value, col.Width)
		if col.Title == "STATUS" {
			cell = statusStyle(value).Render(cell)
		}
		cells = append(cells, cell)
	}
	return strings.Join(cells, "  ")
}

func tableWidth(columns []table.Column) int {
	width := 0
	for idx, col := range columns {
		width += col.Width
		if idx > 0 {
			width += 2
		}
	}
	return width
}

func fitCell(value string, width int) string {
	if width <= 0 {
		return ""
	}
	value = truncateCell(value, width)
	if pad := width - lipgloss.Width(value); pad > 0 {
		value += strings.Repeat(" ", pad)
	}
	return value
}

func truncateCell(value string, width int) string {
	if lipgloss.Width(value) <= width {
		return value
	}
	if width == 1 {
		return "…"
	}
	var b strings.Builder
	for _, r := range value {
		next := b.String() + string(r)
		if lipgloss.Width(next) > width-1 {
			break
		}
		b.WriteRune(r)
	}
	return b.String() + "…"
}

func (m Model) SaveState() error {
	return state.Save(m.cachePath, m.runner.Cache())
}

func (m Model) queueRefresh() (Model, tea.Cmd) {
	if m.refreshing {
		m.pending = true
		return m, nil
	}
	m.refreshing = true
	return m, m.refreshCmd()
}

func (m Model) refreshCmd() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		snapshot, err := m.runner.Refresh(ctx)
		return refreshMsg{snapshot: snapshot, err: err}
	}
}

func tick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m *Model) applyRows() {
	rows := make([]table.Row, 0, len(m.rows))
	repoRows := rowsByRepo(m.rows)
	contextRows := rowsByContext(m.rows)
	seenRepos := map[string]int{}
	seenContexts := map[string]int{}
	for _, row := range m.rows {
		repo := repoCell(row, seenRepos[row.Repo], repoRows[row.Repo])
		contextKey := rowContextKey(row)
		context := contextCell(row, seenContexts[contextKey])
		workflow := workflowCell(row, seenContexts[contextKey], contextRows[contextKey])
		rows = append(rows, table.Row{
			repo, context, workflow, statusLabel(row.Status), displayRef(row),
			duration(row.StartedAt, row.FinishedAt), titleSHA(row, context),
		})
		seenRepos[row.Repo]++
		seenContexts[contextKey]++
	}
	m.table.SetRows(rows)
}

func (m *Model) sendNotifications(rows []Row) {
	for _, row := range rows {
		if row.Kind != RowRun || row.Status != StatusBroken || row.Error != "" || !row.Notify {
			continue
		}
		n := notify.Notification{Repo: row.Repo, Workflow: row.Workflow, Branch: row.Branch, Title: row.Title}
		if err := m.notifier.MacOS(m.cfg.NotifyMacOS, n); err != nil && !m.notifiedOSFailure {
			m.notifiedOSFailure = true
			m.addEvent("macOS notification failed: " + err.Error())
		}
	}
}

func (m *Model) openSelected() error {
	idx := m.table.Cursor()
	if idx < 0 || idx >= len(m.rows) {
		return nil
	}
	if m.rows[idx].ContextURL != "" {
		return m.notifier.Open(m.rows[idx].ContextURL)
	}
	return m.notifier.Open(m.rows[idx].URL)
}

func (m *Model) addEvent(event string) {
	if event == "" {
		return
	}
	m.events = append(m.events, time.Now().Format("15:04:05")+" "+event)
	if len(m.events) > 20 {
		m.events = m.events[len(m.events)-20:]
	}
}

func (m *Model) resizeColumns(width int) {
	if width <= 0 {
		return
	}
	fixed := 18 + 12 + 9
	remaining := max(20, width-fixed-10)
	context := min(34, max(18, remaining/3))
	workflow := min(32, max(16, remaining/4))
	ref := min(22, max(12, remaining/5))
	title := max(16, remaining-context-workflow-ref)
	m.table.SetColumns([]table.Column{
		{Title: "REPO", Width: 18}, {Title: "CONTEXT", Width: context}, {Title: "WORKFLOW", Width: workflow},
		{Title: "STATUS", Width: 12}, {Title: "REF", Width: ref},
		{Title: "DURATION", Width: 9}, {Title: "TITLE/SHA", Width: title},
	})
}

func rowsByRepo(rows []Row) map[string]int {
	counts := map[string]int{}
	for _, row := range rows {
		counts[row.Repo]++
	}
	return counts
}

func rowsByContext(rows []Row) map[string]int {
	counts := map[string]int{}
	for _, row := range rows {
		counts[rowContextKey(row)]++
	}
	return counts
}

func repoCell(row Row, idx, _ int) string {
	if idx == 0 {
		return row.Repo
	}
	return ""
}

func contextCell(row Row, idx int) string {
	if idx == 0 {
		return displayContext(row)
	}
	return ""
}

func workflowCell(row Row, idx, total int) string {
	if total <= 1 {
		return row.Workflow
	}
	if idx == 0 {
		return "┌ " + row.Workflow
	}
	if idx == total-1 {
		return "└ " + row.Workflow
	}
	return "├ " + row.Workflow
}

type statusSummary struct {
	Broken  int
	Running int
	OK      int
	Quiet   int
	Errors  int
}

func statusSummaryForRows(rows []Row) statusSummary {
	var summary statusSummary
	for _, row := range rows {
		switch row.Status {
		case StatusBroken:
			summary.Broken++
		case StatusRunning:
			summary.Running++
		case StatusOK:
			summary.OK++
		case StatusError:
			summary.Errors++
		default:
			summary.Quiet++
		}
	}
	return summary
}

func renderStatusSummary(summary statusSummary) string {
	parts := []string{
		summaryBadge("BROKEN", summary.Broken, errorStyle()),
		summaryBadge("RUNNING", summary.Running, activeStyle()),
		summaryBadge("OK", summary.OK, successStyle()),
		summaryBadge("QUIET", summary.Quiet, quietStyle()),
		summaryBadge("ERRORS", summary.Errors, errorStyle()),
	}
	return strings.Join(parts, "  ")
}

func summaryBadge(label string, count int, style lipgloss.Style) string {
	return style.Bold(true).Render(fmt.Sprintf("%s %d", label, count))
}

func statusLabel(s Status) string {
	switch s {
	case StatusBroken:
		return "✖ BROKEN"
	case StatusRunning:
		return "… RUNNING"
	case StatusOK:
		return "✓ OK"
	case StatusNeutral:
		return "• QUIET"
	default:
		return "! ERROR"
	}
}

func tableStyles() table.Styles {
	styles := table.DefaultStyles()
	styles.Header = styles.Header.
		Bold(true).
		Foreground(lipgloss.Color("14")).
		BorderStyle(lipgloss.NormalBorder()).
		BorderBottom(true).
		BorderForeground(lipgloss.Color("8"))
	styles.Cell = styles.Cell.Foreground(lipgloss.Color("252"))
	styles.Selected = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("14")).
		Background(lipgloss.Color("236")).
		Padding(0, 1)
	return styles
}

func headerCellStyle() lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("14"))
}

func selectedRowStyle() lipgloss.Style {
	return lipgloss.NewStyle().Background(lipgloss.Color("236")).Foreground(lipgloss.Color("15"))
}

func statusStyle(label string) lipgloss.Style {
	switch {
	case strings.Contains(label, "BROKEN"), strings.Contains(label, "ERROR"):
		return errorStyle().Bold(true)
	case strings.Contains(label, "RUNNING"):
		return activeStyle()
	case strings.Contains(label, "OK"):
		return successStyle()
	default:
		return quietStyle()
	}
}

func titleStyle() lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("14"))
}

func errorStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
}

func activeStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
}

func successStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
}

func quietStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
}

func warningStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color("208")).Bold(true)
}

func helpStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
}

func styledEvent(event string) string {
	if urgentEvent(event) {
		return errorStyle().Render(event)
	}
	if strings.Contains(strings.ToLower(event), "refresh") {
		return activeStyle().Render(event)
	}
	return helpStyle().Render(event)
}

func urgentEvent(event string) bool {
	event = strings.ToLower(event)
	return strings.Contains(event, "broken") ||
		strings.Contains(event, "failed") ||
		strings.Contains(event, "error") ||
		strings.Contains(event, "risk")
}

func Classify(status, conclusion string) Status {
	if status != "completed" {
		return StatusRunning
	}
	switch conclusion {
	case "success":
		return StatusOK
	case "failure", "timed_out", "action_required":
		return StatusBroken
	case "cancelled", "skipped", "neutral":
		return StatusNeutral
	default:
		return StatusNeutral
	}
}

func SortRows(rows []Row) {
	contexts := contextSorts(rows)
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if rank(a.Kind) != rank(b.Kind) {
			return rank(a.Kind) < rank(b.Kind)
		}
		aContext, bContext := contexts[rowContextKey(a)], contexts[rowContextKey(b)]
		if aContext.status != bContext.status {
			return aContext.status < bContext.status
		}
		if !aContext.updated.Equal(bContext.updated) {
			return aContext.updated.After(bContext.updated)
		}
		if aContext.key != bContext.key {
			return aContext.key < bContext.key
		}
		if a.Repo != b.Repo {
			return a.Repo < b.Repo
		}
		return a.Workflow < b.Workflow
	})
}

func SortRowsByRepoOrder(rows []Row, repos []string) {
	order := make(map[string]int, len(repos))
	for idx, repo := range repos {
		order[NormalizeRepo(repo)] = idx
	}
	contexts := contextSorts(rows)
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		aRank, aKnown := order[NormalizeRepo(a.Repo)]
		bRank, bKnown := order[NormalizeRepo(b.Repo)]
		if aKnown != bKnown {
			return aKnown
		}
		if aKnown && aRank != bRank {
			return aRank < bRank
		}
		if !aKnown && a.Repo != b.Repo {
			return a.Repo < b.Repo
		}
		if rank(a.Kind) != rank(b.Kind) {
			return rank(a.Kind) < rank(b.Kind)
		}
		aContext, bContext := contexts[rowContextKey(a)], contexts[rowContextKey(b)]
		if aContext.status != bContext.status {
			return aContext.status < bContext.status
		}
		if !aContext.updated.Equal(bContext.updated) {
			return aContext.updated.After(bContext.updated)
		}
		if aContext.key != bContext.key {
			return aContext.key < bContext.key
		}
		return a.Workflow < b.Workflow
	})
}

type contextSort struct {
	key     string
	status  int
	updated time.Time
}

func contextSorts(rows []Row) map[string]contextSort {
	contexts := map[string]contextSort{}
	for _, row := range rows {
		key := rowContextKey(row)
		current, ok := contexts[key]
		next := contextSort{key: key, status: statusRank(row.Status), updated: row.UpdatedAt}
		if !ok || next.status < current.status || next.updated.After(current.updated) {
			if ok && next.status > current.status {
				next.status = current.status
			}
			if ok && current.updated.After(next.updated) {
				next.updated = current.updated
			}
			contexts[key] = next
		}
	}
	return contexts
}

func statusRank(status Status) int {
	switch status {
	case StatusError, StatusBroken:
		return 0
	case StatusRunning:
		return 1
	case StatusNeutral:
		return 2
	default:
		return 3
	}
}

func rank(k RowKind) int {
	switch k {
	case RowError:
		return 0
	case RowRun:
		return 1
	default:
		return 2
	}
}

func displayRef(row Row) string {
	if row.Branch != "" {
		return row.Branch
	}
	return row.SHA
}

func displayContext(row Row) string {
	if row.Context != "" {
		return row.Context
	}
	if row.Kind == RowError || row.Kind == RowNoRuns {
		return "-"
	}
	if row.Event == "push" {
		return displayRef(row) + " direct push"
	}
	if row.Event != "" {
		return displayRef(row) + " " + row.Event
	}
	return displayRef(row)
}

func rowContextKey(row Row) string {
	if row.ContextKey != "" {
		return row.Repo + "\x00" + row.ContextKey
	}
	return row.Repo + "\x00" + displayContext(row)
}

func titleSHA(row Row, context string) string {
	if row.Title != "" {
		if context != "" && strings.Contains(context, row.Title) {
			return ""
		}
		return row.Title
	}
	if len(row.SHA) >= 7 {
		return row.SHA[:7]
	}
	return row.SHA
}

func age(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return until(t)
}

func duration(start, end time.Time) string {
	if start.IsZero() {
		return "-"
	}
	if end.IsZero() {
		end = time.Now()
	}
	return end.Sub(start).Round(time.Second).String()
}

func until(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	d := time.Until(t).Round(time.Second)
	if d < 0 {
		d = -d
	}
	return d.String()
}

func (m Model) rateText() string {
	if m.rate.Limit == 0 {
		return "unknown"
	}
	reset := "-"
	if !m.rate.Reset.IsZero() {
		reset = m.rate.Reset.Format("15:04")
	}
	return fmt.Sprintf("%d/%d reset:%s projected:%.0f%%", m.rate.Remaining, m.rate.Limit, reset, m.rate.Projected*100)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
