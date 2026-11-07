// Package tui implements the interactive terminal interface of nali.
package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/coolxitech/nali/internal/db"
	"github.com/coolxitech/nali/internal/progress"
	"github.com/coolxitech/nali/pkg/entity"
)

const (
	// maxBlocks is the number of query blocks kept in the result area; older
	// blocks are dropped when a new one arrives.
	maxBlocks = 200
	// defaultWidth and defaultHeight are used until the terminal size is known,
	// so that View can always render a full frame instead of panicking.
	defaultWidth  = 80
	defaultHeight = 24
	// minResultHeight keeps the result area usable on very small terminals.
	minResultHeight = 1
	// maxHistory is the number of query lines recalled with ↑/↓.
	maxHistory = 500
	// inputCharLimit bounds the length of a query line.
	inputCharLimit = 4096
)

// Options configures the TUI.
type Options struct {
	Version   string                                     // shown in the title bar, e.g. constant.Version
	Lookup    func(line string) (entity.Entities, error) // nil → default entity.ParseLineWith(line, db.FindOrError)
	Databases []DatabaseInfo                             // status bar
	// Update runs the database update and reports progress through emit until
	// it finishes or ctx is cancelled. A nil Update hides the update panel.
	Update func(ctx context.Context, emit func(db.UpdateEvent))
	// Refresh returns the database status shown in the header; it is called
	// once an update finishes.
	Refresh func() []DatabaseInfo
	// Reset drops the opened databases and cached lookups after an update, so
	// the next query reads the new files. It is called once no lookup is in
	// flight.
	Reset func()
}

// DatabaseInfo describes one selected database for the status bar.
type DatabaseInfo struct {
	Kind  string   // "IPv4" / "IPv6" / "CDN"
	Names []string // 已选库名（可能多个，逗号分隔而来）
	Ready bool     // 文件都存在
	Note  string   // Ready 为 false 时展示，如 "文件缺失，请先执行 nali update"
}

// lookupDoneMsg is the result of one asynchronous lookup.
type lookupDoneMsg struct {
	seq  int
	line string
	es   entity.Entities
	err  error
}

// focusArea is the part of the interface that currently owns the keyboard.
type focusArea int

const (
	focusInput focusArea = iota
	focusResults
)

// queryBlock is one submitted line together with its (possibly pending)
// results.
type queryBlock struct {
	seq  int
	line string
	es   entity.Entities
	err  error
	done bool
}

// answer is one database answer of one entity.
type answer struct {
	source string
	text   string
}

// Model is the Bubble Tea model of the TUI. It implements tea.Model.
type Model struct {
	opts   Options
	styles styles
	keys   keyMap

	width  int
	height int
	ready  bool

	input   textinput.Model
	spinner spinner.Model
	help    help.Model

	focus    focusArea
	showHelp bool

	blocks  []*queryBlock
	history []string
	histPos int

	nextSeq int
	// pending is the number of submitted queries that have not been answered
	// yet: the one being looked up plus the ones still waiting in the queue.
	pending int
	// runningSeq is the seq of the query being looked up, or -1 when the queue
	// is idle. Lookups run one at a time because the database bindings are not
	// safe for concurrent use.
	runningSeq int
	// spinning reports whether a spinner tick is already scheduled, so that
	// queued queries share one animation.
	spinning bool

	// scroll is the index of the first visible result line. stick keeps the
	// view pinned to the newest result; any manual scroll clears it.
	scroll int
	stick  bool

	// panel is the state of the database update panel. While it is open the
	// panel owns the result area; tracker holds the rows of the shared progress
	// model, updateCh the events of the running update and updateCancel stops
	// it.
	panel           panelState
	tracker         *progress.Tracker
	updateCh        <-chan db.UpdateEvent
	updateCancel    context.CancelFunc
	updateCancelled bool
	// pendingStart reports that the update was confirmed while a lookup was
	// still using a database; the run starts as soon as the queue is empty.
	pendingStart bool
	// now is the clock the download speed is measured with; tests replace it.
	now func() time.Time
}

// New creates the TUI model. A nil Lookup falls back to the real databases.
func New(opts Options) *Model {
	if opts.Lookup == nil {
		opts.Lookup = defaultLookup
	}

	s := newStyles()

	input := textinput.New()
	input.Prompt = "> "
	input.Placeholder = "输入 IP / IPv6 / 域名，或粘贴任意一行日志，enter 查询"
	input.PromptStyle = s.prompt
	input.TextStyle = s.plain
	input.PlaceholderStyle = s.placeholder
	input.Cursor.Style = s.prompt
	input.CharLimit = inputCharLimit
	input.Width = defaultWidth - lipgloss.Width(input.Prompt) - 1
	input.Focus()

	h := help.New()
	h.ShortSeparator = " · "
	h.Width = defaultWidth

	return &Model{
		opts:       opts,
		styles:     s,
		keys:       newKeyMap(),
		input:      input,
		spinner:    spinner.New(spinner.WithSpinner(spinner.Dot), spinner.WithStyle(s.kind)),
		help:       h,
		focus:      focusInput,
		stick:      true,
		runningSeq: -1,
		now:        time.Now,
		// Init schedules the first tick, see Init
		spinning: true,
	}
}

// defaultLookup resolves a line against the configured databases without ever
// terminating the process; failures are reported to the caller instead.
func defaultLookup(line string) (entity.Entities, error) {
	return entity.ParseLineWith(line, db.FindOrError)
}

// Init implements tea.Model.
func (m *Model) Init() tea.Cmd {
	return tea.Batch(m.input.Focus(), m.spinner.Tick)
}

// Update implements tea.Model. It never performs blocking I/O: the lookup runs
// in the goroutine Bubble Tea starts for the returned tea.Cmd, and only one
// lookup is ever in flight (see submit and startNext).
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.resize(msg.Width, msg.Height)
		return m, nil
	case tea.KeyMsg:
		return m.handleKey(msg)
	case lookupDoneMsg:
		return m, m.applyLookup(msg)
	case updateEventMsg:
		return m, m.applyUpdateEvent(db.UpdateEvent(msg))
	case updateClosedMsg:
		if m.panel == panelRunning {
			// the run ended without a StageFinished event: its goroutine
			// returned after a cancellation, or the update was a stub. Finish
			// anyway so that the panel never stays in running.
			m.finishUpdate()
		}
		return m, nil
	case spinner.TickMsg:
		if m.pending == 0 {
			// stop the animation while nothing is in flight
			m.spinning = false
			return m, nil
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}
	return m, nil
}

// View implements tea.Model.
func (m *Model) View() string {
	width := m.effectiveWidth()

	lines := make([]string, 0, m.effectiveHeight())
	lines = append(lines, m.titleLine(width))
	lines = append(lines, m.dbNotes()...)

	body := m.bodyLines()
	top := m.scrollTop(len(body))
	height := m.resultHeight()
	for i := 0; i < height; i++ {
		if idx := top + i; idx < len(body) {
			lines = append(lines, body[idx])
		} else {
			lines = append(lines, "")
		}
	}

	lines = append(lines, m.inputLine(), m.hintLine())

	for i, line := range lines {
		lines[i] = ansi.Truncate(line, width, "…")
	}
	return strings.Join(lines, "\n")
}

// resize records the terminal size reported by Bubble Tea.
func (m *Model) resize(width, height int) {
	if width > 0 {
		m.width = width
	}
	if height > 0 {
		m.height = height
	}
	m.ready = m.width > 0 && m.height > 0
	m.help.Width = m.effectiveWidth()
	m.input.Width = m.effectiveWidth() - lipgloss.Width(m.input.Prompt) - 1
	if m.input.Width < 1 {
		m.input.Width = 1
	}
	m.clampScroll()
}

// effectiveWidth is the width used for rendering, with a fallback for the time
// before the first WindowSizeMsg.
func (m *Model) effectiveWidth() int {
	if m.width > 0 {
		return m.width
	}
	return defaultWidth
}

// effectiveHeight is the height used for rendering, with a fallback for the
// time before the first WindowSizeMsg.
func (m *Model) effectiveHeight() int {
	if m.height > 0 {
		return m.height
	}
	return defaultHeight
}

// fixedLines is the number of lines used by the title, the database notes, the
// input and the hint; the rest of the terminal belongs to the result area.
func (m *Model) fixedLines() int {
	return 3 + len(m.dbNotes())
}

// resultHeight is the number of lines the result area shows.
func (m *Model) resultHeight() int {
	height := m.effectiveHeight() - m.fixedLines()
	if height < minResultHeight {
		return minResultHeight
	}
	return height
}

// handleKey dispatches one key press.
func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// ctrl+c always quits, even while the help overlay is open
	if msg.String() == "ctrl+c" {
		// a running update must not outlive the program
		if m.updateCancel != nil {
			m.updateCancel()
		}
		return m, tea.Quit
	}

	if m.showHelp {
		switch msg.String() {
		case "?", "esc":
			m.closeHelp()
		}
		// ordinary input is ignored while the help overlay is open
		return m, nil
	}

	// the update panel only takes the keys it needs: the input stays usable
	// while it is open
	if m.panel != panelClosed {
		switch msg.String() {
		case "esc":
			if m.panel == panelRunning {
				// a running update is cancelled, but the panel stays open until
				// the run reports its end
				m.cancelUpdate()
				return m, nil
			}
			m.closeUpdatePanel()
			return m, nil
		case "enter":
			switch m.panel {
			case panelConfirm:
				// starts the run, or waits for the query queue to drain when a
				// lookup still uses a database
				return m, m.startUpdate()
			case panelRunning:
				// A query submitted now would open a database the update is
				// about to replace, and on Windows the replacement would then
				// fail; the line stays in the input until the run is over.
				return m, nil
			case panelFinished:
				// the panel covers the results, so a query submitted here would
				// run unseen; esc closes the panel and the line stays in the
				// input until then
				return m, nil
			}
		}
	}

	switch msg.String() {
	case "?":
		// a question mark has to stay typable in the middle of a query, so the
		// help only opens when the input cannot take the character
		if m.focus == focusResults || m.input.Value() == "" {
			m.openHelp()
			return m, nil
		}
	case "u":
		// the update panel follows exactly the same rule as the help overlay
		if m.panel == panelClosed && m.opts.Update != nil &&
			(m.focus == focusResults || m.input.Value() == "") {
			m.openUpdatePanel()
			return m, nil
		}
	case "esc":
		// first close the help, then clear the input, then quit
		if m.input.Value() != "" {
			m.input.SetValue("")
			m.histPos = len(m.history)
			return m, nil
		}
		return m, tea.Quit
	case "tab", "shift+tab":
		return m, m.toggleFocus()
	case "pgup", "ctrl+u":
		m.scrollPage(-1)
		return m, nil
	case "pgdown", "ctrl+d":
		m.scrollPage(1)
		return m, nil
	case "ctrl+l":
		m.clearResults()
		return m, nil
	case "up":
		if m.focus == focusResults {
			m.scrollLine(-1)
		} else {
			m.historyPrev()
		}
		return m, nil
	case "down":
		if m.focus == focusResults {
			m.scrollLine(1)
		} else {
			m.historyNext()
		}
		return m, nil
	case "enter":
		if m.focus != focusInput {
			return m, nil
		}
		return m.submit()
	}

	if m.focus != focusInput {
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// openHelp shows the help overlay from its first line, so that a terminal too
// small for the whole key table does not open it at the bottom.
func (m *Model) openHelp() {
	m.showHelp = true
	m.scroll = 0
	m.stick = false
}

// closeHelp hides the help overlay and follows the newest result again.
func (m *Model) closeHelp() {
	m.showHelp = false
	m.stick = true
}

// submit queues a lookup for the current input and starts it when the queue is
// idle, so that only one lookup runs at a time.
func (m *Model) submit() (tea.Model, tea.Cmd) {
	line := strings.TrimSpace(m.input.Value())
	if line == "" {
		return m, nil
	}

	if len(m.history) == 0 || m.history[len(m.history)-1] != line {
		m.history = append(m.history, line)
		if len(m.history) > maxHistory {
			m.history = m.history[len(m.history)-maxHistory:]
		}
	}
	m.histPos = len(m.history)
	m.input.SetValue("")

	seq := m.nextSeq
	m.nextSeq++
	m.blocks = append(m.blocks, &queryBlock{seq: seq, line: line})
	m.trimBlocks()

	m.pending++
	m.stick = true

	cmds := make([]tea.Cmd, 0, 2)
	// the queue is idle, so the new query can run right away; otherwise it
	// waits for the answer of the query in front of it
	if cmd := m.startNext(); cmd != nil {
		cmds = append(cmds, cmd)
	}
	if !m.spinning {
		m.spinning = true
		cmds = append(cmds, m.spinner.Tick)
	}
	return m, tea.Batch(cmds...)
}

// trimBlocks drops the oldest query blocks beyond maxBlocks. Queries that were
// still queued never run, so they stop counting as pending; the answer of a
// dropped query that is already in flight is simply ignored when it arrives.
func (m *Model) trimBlocks() {
	if len(m.blocks) <= maxBlocks {
		return
	}
	drop := len(m.blocks) - maxBlocks
	for _, b := range m.blocks[:drop] {
		if !b.done && b.seq != m.runningSeq && m.pending > 0 {
			m.pending--
		}
	}
	m.blocks = m.blocks[drop:]
}

// startNext dispatches the oldest query that has not been answered yet and
// reports whether it did. The queue is only touched when no lookup is running.
func (m *Model) startNext() tea.Cmd {
	if m.runningSeq >= 0 {
		return nil
	}
	for _, b := range m.blocks {
		if b.done {
			continue
		}
		m.runningSeq = b.seq
		return lookupCmd(m.opts.Lookup, b.seq, b.line)
	}
	return nil
}

// lookupCmd runs one lookup in the goroutine Bubble Tea starts for it.
func lookupCmd(lookup func(string) (entity.Entities, error), seq int, line string) tea.Cmd {
	return func() tea.Msg {
		es, err := lookup(line)
		return lookupDoneMsg{seq: seq, line: line, es: es, err: err}
	}
}

// applyLookup stores an answer in its block and dispatches the next queued
// query. The block was appended when the query was submitted, so the results
// stay in submission order.
func (m *Model) applyLookup(msg lookupDoneMsg) tea.Cmd {
	// an answer for a query that is not in flight is stale and must not touch
	// the queue bookkeeping
	if m.runningSeq != msg.seq {
		return nil
	}
	m.runningSeq = -1
	if m.pending > 0 {
		m.pending--
	}
	m.stick = true

	// the block may be gone already: ctrl+l dropped it, or it was trimmed away
	// while it was still in flight; its answer is then simply not shown
	for _, b := range m.blocks {
		if b.seq != msg.seq {
			continue
		}
		// the line is part of the identity of a submission; a mismatch means
		// the answer belongs to another block
		if b.line == msg.line {
			b.es, b.err, b.done = msg.es, msg.err, true
		}
		break
	}

	// An update confirmed while the queue was busy may start now: no lookup
	// uses a database any more, so the files can be replaced and the opened
	// databases dropped.
	if m.pendingStart && m.pending == 0 && m.runningSeq == -1 {
		return m.startUpdate()
	}

	if m.pending > 0 {
		return m.startNext()
	}
	return nil
}

// toggleFocus switches between the input and the result area.
func (m *Model) toggleFocus() tea.Cmd {
	if m.focus == focusInput {
		m.focus = focusResults
		m.input.Blur()
		return nil
	}
	m.focus = focusInput
	return m.input.Focus()
}

// historyPrev recalls the previous query line.
func (m *Model) historyPrev() {
	if m.histPos <= 0 || len(m.history) == 0 {
		return
	}
	m.histPos--
	m.input.SetValue(m.history[m.histPos])
	m.input.CursorEnd()
}

// historyNext walks back towards the newest query line and clears the input
// once it is reached.
func (m *Model) historyNext() {
	if m.histPos >= len(m.history) {
		return
	}
	m.histPos++
	if m.histPos >= len(m.history) {
		m.input.SetValue("")
		return
	}
	m.input.SetValue(m.history[m.histPos])
	m.input.CursorEnd()
}

// clearResults drops every result block but keeps the query history. Queries
// that were still queued never run, so only the one already in flight keeps
// counting as pending until its answer arrives.
func (m *Model) clearResults() {
	m.blocks = nil
	if m.runningSeq >= 0 {
		m.pending = 1
	} else {
		m.pending = 0
	}
	m.scroll = 0
	m.stick = true
}

// scrollLine moves the result view by one line.
func (m *Model) scrollLine(delta int) {
	m.scrollTo(m.scrollTop(len(m.bodyLines())) + delta)
}

// scrollPage moves the result view by one page.
func (m *Model) scrollPage(delta int) {
	step := m.resultHeight() - 1
	if step < 1 {
		step = 1
	}
	m.scrollTo(m.scrollTop(len(m.bodyLines())) + delta*step)
}

// scrollTo clamps the scroll offset and pins the view to the bottom again when
// the last line is visible.
func (m *Model) scrollTo(top int) {
	maxTop := m.maxTopFor(len(m.bodyLines()))
	if top < 0 {
		top = 0
	}
	if top > maxTop {
		top = maxTop
	}
	m.scroll = top
	m.stick = top >= maxTop
}

// clampScroll keeps the scroll offset valid after a resize or new results.
func (m *Model) clampScroll() {
	if maxTop := m.maxTopFor(len(m.bodyLines())); m.scroll > maxTop {
		m.scroll = maxTop
	}
	if m.scroll < 0 {
		m.scroll = 0
	}
}

// maxTopFor is the largest valid scroll offset for a body of total lines.
func (m *Model) maxTopFor(total int) int {
	if maxTop := total - m.resultHeight(); maxTop > 0 {
		return maxTop
	}
	return 0
}

// scrollTop is the index of the first visible result line for a body of total
// lines.
func (m *Model) scrollTop(total int) int {
	maxTop := m.maxTopFor(total)
	if m.stick || m.scroll > maxTop {
		return maxTop
	}
	return m.scroll
}

// titleLine renders the title bar: name and version on the left, the selected
// databases on the right.
func (m *Model) titleLine(width int) string {
	left := m.styles.title.Render("nali")
	if m.opts.Version != "" {
		left += " " + m.styles.version.Render(m.opts.Version)
	}
	right := m.dbStatus()
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		return left
	}
	return left + strings.Repeat(" ", gap) + right
}

// dbStatus renders the database summary of the title bar.
func (m *Model) dbStatus() string {
	parts := make([]string, 0, len(m.opts.Databases))
	for _, info := range m.opts.Databases {
		label := info.Kind
		if names := strings.Join(info.Names, ","); names != "" {
			label += " " + names
		}
		if info.Ready {
			parts = append(parts, m.styles.statusOK.Render(label+" ✓"))
			continue
		}
		parts = append(parts, m.styles.statusBad.Render(label+" ✗"))
	}
	return strings.Join(parts, "  ")
}

// dbNotes lists one warning line per database that is not ready.
func (m *Model) dbNotes() []string {
	var notes []string
	for _, info := range m.opts.Databases {
		if info.Ready {
			continue
		}
		label := info.Kind
		if names := strings.Join(info.Names, ","); names != "" {
			label += " " + names
		}
		note := info.Note
		if note == "" {
			note = "数据库不可用，请先执行 nali update"
		}
		notes = append(notes, m.styles.note.Render("! "+label+"："+note))
	}
	return notes
}

// inputLine renders the prompt and the text input.
func (m *Model) inputLine() string {
	return m.input.View()
}

// hintLine renders the spinner while queries are running and the key hints.
func (m *Model) hintLine() string {
	hint := m.help.View(m.keys)
	if m.pending > 0 {
		hint = m.spinner.View() + " " + m.styles.kind.Render(fmt.Sprintf("查询中 %d", m.pending)) + " · " + hint
	}
	return m.styles.hint.Render(hint)
}

// bodyLines renders what the scrollable area shows: the help overlay, the update
// panel, the initialising placeholder or the query blocks.
func (m *Model) bodyLines() []string {
	if m.showHelp {
		return strings.Split(m.help.FullHelpView(m.keys.FullHelp()), "\n")
	}
	if m.panel != panelClosed {
		return m.updateLines()
	}
	if len(m.blocks) == 0 {
		if !m.ready {
			return []string{m.styles.placeholder.Render("初始化中…")}
		}
		return []string{m.styles.placeholder.Render("还没有查询结果：输入 IP、IPv6 或域名后按 enter")}
	}
	var lines []string
	for _, block := range m.blocks {
		lines = append(lines, m.blockLines(block)...)
	}
	return lines
}

// blockLines renders one query block.
func (m *Model) blockLines(b *queryBlock) []string {
	lines := []string{m.blockHeader(b)}

	if !b.done {
		state := "查询中…"
		if b.seq != m.runningSeq {
			// lookups run one at a time, so this query is still waiting
			state = "排队中…"
		}
		lines = append(lines, "   "+m.spinner.View()+" "+m.styles.kind.Render(state))
		return lines
	}
	if b.err != nil {
		lines = append(lines, "   "+m.styles.failure.Render("库不可用："+b.err.Error()))
		lines = append(lines, "   "+m.styles.placeholder.Render("若数据库文件缺失或损坏，可执行 nali update 重新下载"))
	}

	recognised := 0
	for _, e := range b.es {
		if e == nil || e.Type == entity.TypePlain {
			continue
		}
		recognised++
		lines = append(lines, "   "+m.styles.entityStyle(e.Type).Render(e.Text)+"  "+m.styles.kind.Render(typeLabel(e.Type)))
		for _, a := range entityAnswers(e) {
			lines = append(lines, "     "+m.styles.source.Render(a.source)+"  "+m.styles.answer.Render(a.text))
		}
	}
	if recognised == 0 && b.err == nil {
		// an entity the databases could not answer is reported as plain text by
		// entity.ParseLineWith, so both cases end up here
		lines = append(lines, "   "+m.styles.placeholder.Render("未在输入中识别到可查询的 IP / 域名，或所选数据库没有对应记录"))
	}
	return lines
}

// blockHeader renders the submitted line with its entities coloured.
func (m *Model) blockHeader(b *queryBlock) string {
	index := m.styles.index.Render(fmt.Sprintf("%d.", b.seq+1))
	return index + " " + m.coloredLine(b)
}

// coloredLine colours the entities of a submitted line. The entities cover the
// whole line, including its plain parts.
func (m *Model) coloredLine(b *queryBlock) string {
	if len(b.es) == 0 {
		return b.line
	}
	var raw, out strings.Builder
	for _, e := range b.es {
		if e == nil {
			continue
		}
		raw.WriteString(e.Text)
		out.WriteString(m.styles.entityStyle(e.Type).Render(e.Text))
	}
	if raw.String() != b.line {
		// the entities do not describe the line, so do not lose any of it
		return b.line
	}
	return out.String()
}

// entityAnswers returns the database answers of one entity: every selected
// database when several answered, the primary one otherwise.
func entityAnswers(e *entity.Entity) []answer {
	if len(e.Results) > 0 {
		answers := make([]answer, 0, len(e.Results))
		for _, r := range e.Results {
			if r.Text == "" {
				continue
			}
			answers = append(answers, answer{source: r.Source, text: r.Text})
		}
		return answers
	}
	if e.InfoText == "" {
		return nil
	}
	return []answer{{source: e.Source, text: e.InfoText}}
}

// typeLabel names an entity type the way the TUI shows it.
func typeLabel(t entity.EntityType) string {
	switch t {
	case entity.TypeIPv4:
		return "IPv4"
	case entity.TypeIPv6:
		return "IPv6"
	case entity.TypeDomain:
		return "Domain"
	default:
		return ""
	}
}
