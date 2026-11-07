package tui

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/zu1k/nali/internal/db"
	"github.com/zu1k/nali/internal/progress"
)

const (
	// minBarWidth and maxBarWidth bound the progress bar of the update panel, so
	// that it stays readable on a narrow terminal and never eats the whole line
	// on a wide one.
	minBarWidth = 10
	maxBarWidth = 40
	// percentWidth is the room the percentage next to the progress bar needs.
	percentWidth = 4
	// updateIndent indents the message lines below a database name.
	updateIndent = "  "
)

// panelState is the state of the update panel. The progress of the run itself is
// the shared progress model, so this only adds whether the panel is on screen.
type panelState int

const (
	// panelClosed hides the panel: the result area shows the queries again.
	panelClosed panelState = iota
	// panelConfirm lists the databases the update is going to refresh and waits
	// for the user to start it.
	panelConfirm
	// panelRunning shows the progress of the running update.
	panelRunning
	// panelFinished shows the summary of a finished or cancelled update.
	panelFinished
)

// progressState is the state the shared progress model describes the run in. The
// closed panel shows no run, so it borrows the confirmation state, which is the
// one StageText does not read anything from.
func (s panelState) progressState() progress.State {
	switch s {
	case panelRunning:
		return progress.StateRunning
	case panelFinished:
		return progress.StateFinished
	default:
		return progress.StateConfirm
	}
}

// updateEventMsg carries one event of the running update into the model.
type updateEventMsg db.UpdateEvent

// updateClosedMsg reports that the update goroutine closed its channel, so that
// a run that ends without a StageFinished event still leaves the panel.
type updateClosedMsg struct{}

// openUpdatePanel shows the panel in its confirmation state, listing the
// databases a plain update refreshes.
func (m *Model) openUpdatePanel() {
	if m.opts.Update == nil || m.panel != panelClosed {
		return
	}
	m.panel = panelConfirm
	m.updateCancelled = false
	m.pendingStart = false
	// The preview is the list a plain update always refreshes. The larger
	// optional databases are only refreshed when they are selected or already
	// downloaded, which the panel cannot know before the run; they appear as
	// soon as the update reports them.
	m.tracker = progress.NewTracker(db.DbNameListForUpdate)
	// the panel owns the whole result area, so it is shown from its first line
	m.scroll = 0
	m.stick = false
}

// closeUpdatePanel hides the panel and gives the result area back to the
// queries. It is only ever reached from the confirmation state and from the
// summary: a running update is cancelled with esc and keeps the panel open
// until it reports its end, and ctrl+c leaves the program, so there is no
// running update whose events would have to be drained here.
func (m *Model) closeUpdatePanel() {
	cancel := m.updateCancel
	m.updateCancel = nil
	if cancel != nil {
		cancel()
	}
	m.updateCh = nil
	m.panel = panelClosed
	m.tracker = nil
	m.updateCancelled = false
	m.pendingStart = false
	m.stick = true
}

// cancelUpdate asks the running update to stop. The panel stays open and shows
// the cancellation until the run reports its end, because the events already on
// their way still belong to it.
func (m *Model) cancelUpdate() {
	if m.updateCancel != nil {
		m.updateCancel()
		m.updateCancel = nil
	}
	m.updateCancelled = true
}

// startUpdate runs the update in its own goroutine and returns the command that
// waits for its first event; Update itself never blocks. It does nothing and
// waits for the query queue instead when a lookup still uses a database.
func (m *Model) startUpdate() tea.Cmd {
	if m.opts.Update == nil || m.panel != panelConfirm {
		return nil
	}

	// Windows refuses to replace a database file that the process still has
	// open, and an opened database keeps its handle, so the update may only
	// start - and the opened databases may only be dropped - while no lookup
	// uses one. The queue is drained first and stays closed while the update
	// runs, see handleKey.
	if m.runningSeq != -1 || m.pending > 0 {
		m.pendingStart = true
		return nil
	}
	m.pendingStart = false
	// Drop the handles of the old files before UpdateDBs renames the new ones
	// into place; a lookup would otherwise make the rename fail on Windows.
	m.resetDatabases()

	ctx, cancel := context.WithCancel(context.Background())
	events := make(chan db.UpdateEvent)

	m.updateCancel = cancel
	m.updateCh = events
	m.panel = panelRunning
	m.updateCancelled = false

	update := m.opts.Update
	go func() {
		// closing the channel is how the end of the run reaches the model, see
		// updateClosedMsg
		defer close(events)
		update(ctx, func(ev db.UpdateEvent) { events <- ev })
	}()
	return waitUpdateCmd(events)
}

// waitUpdateCmd waits for the next event of the update.
func waitUpdateCmd(events <-chan db.UpdateEvent) tea.Cmd {
	if events == nil {
		return nil
	}
	return func() tea.Msg {
		ev, ok := <-events
		if !ok {
			return updateClosedMsg{}
		}
		return updateEventMsg(ev)
	}
}

// applyUpdateEvent stores one event and returns the command that waits for the
// next one. Events are read until the channel is closed, so a run that keeps
// reporting after StageFinished never blocks.
func (m *Model) applyUpdateEvent(ev db.UpdateEvent) tea.Cmd {
	if m.panel == panelClosed || m.updateCh == nil {
		return nil
	}
	next := waitUpdateCmd(m.updateCh)
	if m.panel == panelFinished {
		return next
	}
	if ev.Stage == db.StageFinished {
		m.finishUpdate()
		return next
	}
	// the shared progress model owns the rows: it is fed the events and knows
	// how to describe them, the panel only draws what it returns
	m.tracker.Apply(ev, m.now())
	return next
}

// finishUpdate ends the run: the panel moves to its summary, the databases of
// the title bar are refreshed and the context is released.
func (m *Model) finishUpdate() {
	if m.panel != panelRunning {
		return
	}
	m.panel = panelFinished
	if m.updateCancel != nil {
		m.updateCancel()
		m.updateCancel = nil
	}
	m.tracker.Finish()

	// The update replaced the files on disk, so the opened databases and the
	// cached lookups are stale. No lookup can be in flight here: a run only
	// starts with an empty queue and new queries are not submitted while it
	// runs, see startUpdate and handleKey.
	m.resetDatabases()

	if m.opts.Refresh != nil {
		m.opts.Databases = m.opts.Refresh()
	}
}

// resetDatabases drops the opened databases and the cached lookups of the
// previous files, so that the next query opens the new ones. It must only be
// called while no lookup is in flight, because closing a database that a query
// is still using makes that query fail.
func (m *Model) resetDatabases() {
	if m.opts.Reset != nil {
		m.opts.Reset()
	}
}

// updateLayout is the width of every column of the panel; a width of 0 hides the
// column.
type updateLayout struct {
	name  int
	bar   int
	bytes int
	speed int
	stage int
}

// updateLayout picks the columns that fit into width. The progress bar is the
// first column a narrow terminal loses, then the speed and finally the byte
// counter, so a line is never wider than the terminal.
func (m *Model) updateLayout(width int) updateLayout {
	var l updateLayout
	state := m.panel.progressState()
	for _, row := range m.tracker.Rows() {
		l.name = max(l.name, lipgloss.Width(row.Name))
		l.stage = max(l.stage, lipgloss.Width(row.StageText(state)))
		l.bytes = max(l.bytes, lipgloss.Width(row.BytesText()))
		l.speed = max(l.speed, lipgloss.Width(row.SpeedText()))
	}

	// the room left for the bar once the text columns and the spaces between
	// them are placed
	rest := width - (l.name + 1 + l.stage)
	withSpeed := rest - (1 + l.bytes + 1 + l.speed)
	if m.hasBar() {
		if bar := withSpeed - (1 + 1 + percentWidth); bar >= minBarWidth {
			l.bar = min(bar, maxBarWidth)
			return l
		}
	}
	if withSpeed >= 0 {
		return l
	}
	if rest-(1+l.bytes) >= 0 {
		l.speed = 0
		return l
	}
	l.speed, l.bytes = 0, 0
	return l
}

// hasBar reports whether any row has a download progress to draw, which is what
// decides whether the panel reserves a bar column at all.
func (m *Model) hasBar() bool {
	for _, row := range m.tracker.Rows() {
		if row.ShowsBar() {
			return true
		}
	}
	return false
}

// updateLines renders the update panel: a header, one line per database, the
// summary of a finished run and the hint of the current state.
func (m *Model) updateLines() []string {
	if m.tracker == nil {
		return nil
	}
	l := m.updateLayout(m.effectiveWidth())

	lines := make([]string, 0, len(m.tracker.Rows())+3)
	lines = append(lines, m.styles.title.Render("数据库更新"))
	for _, row := range m.tracker.Rows() {
		lines = append(lines, m.rowLines(row, l)...)
	}
	if m.panel == panelFinished {
		lines = append(lines, m.updateSummary())
	}
	lines = append(lines, m.styles.hint.Render(m.updateHint()))
	return lines
}

// rowLines renders one row of the panel: the columns of the database, followed
// by the indented message lines of a failure. Every column text comes from the
// shared progress model, so the panel says exactly what "nali update" says.
func (m *Model) rowLines(row *progress.Row, l updateLayout) []string {
	filled, empty, lineStyle := m.styles.bar, m.styles.barEmpty, m.styles.plain
	if row.Err != nil && !row.Cancelled() {
		// a failed database is an error from the name to the stage; a cancelled
		// one is not, the user did that on purpose
		filled, empty, lineStyle = m.styles.failure, m.styles.failure, m.styles.failure
	}

	var b strings.Builder
	b.WriteString(lineStyle.Render(padRight(row.Name, l.name)))
	if l.bar > 0 {
		b.WriteString(" ")
		if row.ShowsBar() {
			b.WriteString(barText(row, l.bar, filled, empty))
			b.WriteString(" ")
			b.WriteString(lineStyle.Render(row.PercentText()))
		} else {
			// no progress to show: leave the cells blank so the columns of the
			// other rows stay aligned
			b.WriteString(strings.Repeat(" ", l.bar+1+percentWidth))
		}
	}
	if l.bytes > 0 {
		b.WriteString(" ")
		b.WriteString(lineStyle.Render(padRight(row.BytesText(), l.bytes)))
	}
	if l.speed > 0 {
		b.WriteString(" ")
		b.WriteString(lineStyle.Render(padRight(row.SpeedText(), l.speed)))
	}
	if l.stage > 0 {
		b.WriteString(" ")
		b.WriteString(lineStyle.Render(padRight(row.StageText(m.panel.progressState()), l.stage)))
	}

	lines := []string{b.String()}
	if row.ShowsMessage() {
		style := m.styles.placeholder
		if row.Err != nil {
			style = m.styles.failure
		}
		for _, messageLine := range strings.Split(row.Message, "\n") {
			lines = append(lines, updateIndent+style.Render(messageLine))
		}
	}
	return lines
}

// barText draws the progress bar of row in width cells. The shared model says how
// many cells are filled; the colours belong to the panel.
func barText(row *progress.Row, width int, filled, empty lipgloss.Style) string {
	done, rest := row.BarParts(width)
	return filled.Render(strings.Repeat("█", done)) + empty.Render(strings.Repeat("░", rest))
}

// updateSummary counts the databases of the finished run. The numbers and their
// wording come from the shared progress model; pointing at the errors above is
// something only this panel can do.
func (m *Model) updateSummary() string {
	summary := m.tracker.Summary()
	if m.tracker.Failed() > 0 {
		return m.styles.failure.Render(summary + "，失败原因见上面的错误")
	}
	return m.styles.note.Render(summary)
}

// updateHint is the key hint at the bottom of the panel.
func (m *Model) updateHint() string {
	switch m.panel {
	case panelConfirm:
		if m.pendingStart {
			// the run waits for the query in flight, see startUpdate
			return "等待当前查询完成… · esc 关闭"
		}
		return "enter 开始更新 · esc 关闭"
	case panelRunning:
		if m.updateCancelled {
			// the run keeps reporting until it has seen the cancellation
			return "正在取消…"
		}
		return "更新进行中 · esc 取消"
	case panelFinished:
		return "esc 关闭"
	default:
		return ""
	}
}

// padRight pads s with spaces to width cells.
func padRight(s string, width int) string {
	if w := lipgloss.Width(s); w < width {
		return s + strings.Repeat(" ", width-w)
	}
	return s
}
