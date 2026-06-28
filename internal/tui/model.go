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
		{Title: "REPO", Width: 18}, {Title: "WORKFLOW", Width: 20}, {Title: "STATUS", Width: 12},
		{Title: "REF", Width: 14}, {Title: "EVENT", Width: 10}, {Title: "AGE", Width: 8},
		{Title: "DURATION", Width: 9}, {Title: "TITLE/SHA", Width: 28},
	}
	t := table.New(table.WithColumns(cols), table.WithFocused(true), table.WithHeight(12))
	t.SetStyles(table.DefaultStyles())
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
	header := fmt.Sprintf("ciwatch  repos:%d  rate:%s  next:%s", len(m.cfg.Repos), m.rateText(), until(m.next))
	if m.rate.Warning {
		header += "  RATE RISK"
	}
	if m.refreshing {
		header += "  refreshing"
	}
	b.WriteString(lipgloss.NewStyle().Bold(true).Render(header))
	b.WriteString("\n")
	if m.err != "" {
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Render(m.err))
		b.WriteString("\n")
	}
	b.WriteString(m.table.View())
	b.WriteString("\n")
	if len(m.events) > 0 {
		b.WriteString(strings.Join(m.events[max(0, len(m.events)-3):], "\n"))
		b.WriteString("\n")
	}
	b.WriteString("↑/↓ jk navigate  r refresh  o open  q quit")
	return b.String()
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
	for _, row := range m.rows {
		rows = append(rows, table.Row{
			row.Repo, row.Workflow, statusLabel(row.Status), displayRef(row), row.Event,
			age(row.UpdatedAt), duration(row.StartedAt, row.FinishedAt), titleSHA(row),
		})
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
	fixed := 18 + 12 + 10 + 8 + 9
	remaining := max(20, width-fixed-12)
	workflow := min(24, max(12, remaining/3))
	ref := min(18, max(10, remaining/5))
	title := max(16, remaining-workflow-ref)
	m.table.SetColumns([]table.Column{
		{Title: "REPO", Width: 18}, {Title: "WORKFLOW", Width: workflow}, {Title: "STATUS", Width: 12},
		{Title: "REF", Width: ref}, {Title: "EVENT", Width: 10}, {Title: "AGE", Width: 8},
		{Title: "DURATION", Width: 9}, {Title: "TITLE/SHA", Width: title},
	})
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
		return "• NEUTRAL"
	default:
		return "! ERROR"
	}
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
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if rank(a.Kind) != rank(b.Kind) {
			return rank(a.Kind) < rank(b.Kind)
		}
		if a.Kind == RowRun && !a.UpdatedAt.Equal(b.UpdatedAt) {
			return a.UpdatedAt.After(b.UpdatedAt)
		}
		if a.Repo != b.Repo {
			return a.Repo < b.Repo
		}
		return a.Workflow < b.Workflow
	})
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

func titleSHA(row Row) string {
	if row.Title != "" {
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
