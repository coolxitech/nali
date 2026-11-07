package tui

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/coolxitech/nali/pkg/entity"
)

// stubEntities is the answer the stub lookup returns: the whole line is one
// IPv4 entity answered by a single database.
func stubEntities(line string) entity.Entities {
	return entity.Entities{
		&entity.Entity{
			Loc:      [2]int{0, len(line)},
			Type:     entity.TypeIPv4,
			Text:     line,
			Source:   "czdb",
			InfoText: "美国 加利福尼亚州",
		},
	}
}

// stubLookup answers every line with stubEntities.
func stubLookup(line string) (entity.Entities, error) {
	return stubEntities(line), nil
}

// newTestModel builds a model that never touches a real database.
func newTestModel(lookup func(string) (entity.Entities, error)) *Model {
	m := New(Options{
		Version: "v9.9.9",
		Lookup:  lookup,
		Databases: []DatabaseInfo{
			{Kind: "IPv4", Names: []string{"czdb"}, Ready: true},
		},
	})
	_ = m.Init()
	return m
}

// updateModel feeds msg to the model and returns it as a *Model again.
func updateModel(t *testing.T, m *Model, msg tea.Msg) (*Model, tea.Cmd) {
	t.Helper()
	model, cmd := m.Update(msg)
	next, ok := model.(*Model)
	if !ok {
		t.Fatalf("Update returned %T, want *Model", model)
	}
	return next, cmd
}

// keyMsg builds the message Bubble Tea sends for a special key.
func keyMsg(k tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: k} }

// runeMsg builds the message Bubble Tea sends for a printable key.
func runeMsg(r rune) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}} }

// drain runs cmd and returns every message it produces, batches included.
func drain(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var msgs []tea.Msg
		for _, c := range batch {
			msgs = append(msgs, drain(c)...)
		}
		return msgs
	}
	return []tea.Msg{msg}
}

// submitQuery submits line and runs the lookups it triggers, as Bubble Tea
// would, until the queue is idle again.
func submitQuery(t *testing.T, m *Model, line string) *Model {
	t.Helper()
	m.input.SetValue(line)
	next, cmd := updateModel(t, m, keyMsg(tea.KeyEnter))
	return runQueue(t, next, cmd)
}

// runQueue feeds the messages produced by cmd back into the model, following
// the commands Update returns, until no command is left.
func runQueue(t *testing.T, m *Model, cmd tea.Cmd) *Model {
	t.Helper()
	for cmd != nil {
		var next []tea.Cmd
		for _, msg := range drain(cmd) {
			var c tea.Cmd
			m, c = updateModel(t, m, msg)
			if c != nil {
				next = append(next, c)
			}
		}
		cmd = tea.Batch(next...)
	}
	return m
}

// visibleBody returns the lines the result area currently shows.
func visibleBody(m *Model) []string {
	lines := strings.Split(m.View(), "\n")
	start := 1 + len(m.dbNotes())
	end := start + m.resultHeight()
	if end > len(lines) {
		end = len(lines)
	}
	return lines[start:end]
}

// assertQuit fails unless cmd quits the program.
func assertQuit(t *testing.T, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected a quit command, got nil")
	}
	if msg := cmd(); msg != (tea.QuitMsg{}) {
		t.Fatalf("expected tea.QuitMsg, got %T", msg)
	}
}

func TestViewShowsTitleAndPlaceholder(t *testing.T) {
	m := newTestModel(stubLookup)

	view := m.View()
	for _, want := range []string{"nali", "v9.9.9", "IPv4 czdb", "输入 IP / IPv6 / 域名", "初始化中"} {
		if !strings.Contains(view, want) {
			t.Errorf("View() does not contain %q:\n%s", want, view)
		}
	}
}

func TestSubmitShowsAnswers(t *testing.T) {
	m := newTestModel(stubLookup)
	m = submitQuery(t, m, "8.8.8.8")

	if m.pending != 0 {
		t.Errorf("pending = %d after the answer arrived, want 0", m.pending)
	}
	if len(m.blocks) != 1 || !m.blocks[0].done {
		t.Fatalf("blocks = %d, want one finished block", len(m.blocks))
	}

	view := m.View()
	for _, want := range []string{"8.8.8.8", "IPv4", "czdb", "美国 加利福尼亚州"} {
		if !strings.Contains(view, want) {
			t.Errorf("View() does not contain %q:\n%s", want, view)
		}
	}
}

func TestLookupErrorKeepsModelUsable(t *testing.T) {
	lookup := func(line string) (entity.Entities, error) {
		if line == "1.1.1.1" {
			return nil, errors.New("czdb 未配置解密密钥")
		}
		return stubEntities(line), nil
	}

	m := newTestModel(lookup)
	m = submitQuery(t, m, "1.1.1.1")

	if view := m.View(); !strings.Contains(view, "库不可用：czdb 未配置解密密钥") {
		t.Errorf("View() does not show the lookup error:\n%s", view)
	}
	if view := m.View(); !strings.Contains(view, "若数据库文件缺失或损坏，可执行 nali update 重新下载") {
		t.Errorf("View() does not suggest nali update after an error:\n%s", view)
	}
	if m.pending != 0 {
		t.Errorf("pending = %d after the error, want 0", m.pending)
	}

	// the next query still works
	m = submitQuery(t, m, "8.8.8.8")
	if view := m.View(); !strings.Contains(view, "美国 加利福尼亚州") {
		t.Errorf("the second query did not run:\n%s", view)
	}
}

func TestHistoryNavigation(t *testing.T) {
	m := newTestModel(stubLookup)
	m = submitQuery(t, m, "8.8.8.8")
	m = submitQuery(t, m, "1.1.1.1")

	m, _ = updateModel(t, m, keyMsg(tea.KeyUp))
	if got := m.input.Value(); got != "1.1.1.1" {
		t.Fatalf("first ↑ = %q, want the newest query", got)
	}
	m, _ = updateModel(t, m, keyMsg(tea.KeyUp))
	if got := m.input.Value(); got != "8.8.8.8" {
		t.Fatalf("second ↑ = %q, want the oldest query", got)
	}
	m, _ = updateModel(t, m, keyMsg(tea.KeyDown))
	if got := m.input.Value(); got != "1.1.1.1" {
		t.Fatalf("first ↓ = %q, want the newest query", got)
	}
	m, _ = updateModel(t, m, keyMsg(tea.KeyDown))
	if got := m.input.Value(); got != "" {
		t.Fatalf("↓ past the newest query = %q, want an empty input", got)
	}
}

func TestEscapeLevels(t *testing.T) {
	m := newTestModel(stubLookup)

	// 1. esc closes the help overlay
	m, _ = updateModel(t, m, runeMsg('?'))
	if !m.showHelp {
		t.Fatal("? did not open the help overlay")
	}
	m, cmd := updateModel(t, m, keyMsg(tea.KeyEsc))
	if m.showHelp {
		t.Error("esc did not close the help overlay")
	}
	if cmd != nil {
		t.Error("closing the help overlay returned a command")
	}

	// 2. esc clears a non-empty input
	m.input.SetValue("8.8.8.8")
	m, cmd = updateModel(t, m, keyMsg(tea.KeyEsc))
	if got := m.input.Value(); got != "" {
		t.Errorf("esc left %q in the input, want it cleared", got)
	}
	if cmd != nil {
		t.Error("clearing the input returned a command")
	}

	// 3. esc with an empty input quits
	_, cmd = updateModel(t, m, keyMsg(tea.KeyEsc))
	assertQuit(t, cmd)
}

func TestCtrlCQuits(t *testing.T) {
	m := newTestModel(stubLookup)

	_, cmd := updateModel(t, m, keyMsg(tea.KeyCtrlC))
	assertQuit(t, cmd)

	// ctrl+c also quits while the help overlay is open
	m, _ = updateModel(t, m, runeMsg('?'))
	_, cmd = updateModel(t, m, keyMsg(tea.KeyCtrlC))
	assertQuit(t, cmd)
}

func TestHelpToggle(t *testing.T) {
	m := newTestModel(stubLookup)

	m, _ = updateModel(t, m, runeMsg('?'))
	if !m.showHelp {
		t.Fatal("? did not open the help overlay")
	}
	view := m.View()
	for _, want := range []string{"ctrl+l", "pgup/pgdn", "ctrl+c", "enter"} {
		if !strings.Contains(view, want) {
			t.Errorf("the help overlay does not contain %q:\n%s", want, view)
		}
	}

	// ordinary input is ignored while the help overlay is open
	m, _ = updateModel(t, m, runeMsg('x'))
	if got := m.input.Value(); got != "" {
		t.Errorf("input accepted %q while the help overlay was open", got)
	}

	m, _ = updateModel(t, m, runeMsg('?'))
	if m.showHelp {
		t.Error("? did not close the help overlay")
	}
}

func TestQuestionMarkInInput(t *testing.T) {
	m := newTestModel(stubLookup)

	// an empty input opens the help overlay
	m, _ = updateModel(t, m, runeMsg('?'))
	if !m.showHelp {
		t.Fatal("? with an empty input did not open the help overlay")
	}
	m, _ = updateModel(t, m, runeMsg('?'))
	if m.showHelp {
		t.Fatal("? did not close the help overlay")
	}

	// a question mark inside a query is inserted as text instead
	m.input.SetValue("a")
	m, _ = updateModel(t, m, runeMsg('?'))
	if m.showHelp {
		t.Error("? opened the help overlay although the input was not empty")
	}
	if got, want := m.input.Value(), "a?"; got != want {
		t.Errorf("input = %q, want %q", got, want)
	}

	// the result area keeps the shortcut even with text in the input
	m, _ = updateModel(t, m, keyMsg(tea.KeyTab))
	if m.focus != focusResults {
		t.Fatal("tab did not move the focus to the result area")
	}
	m, _ = updateModel(t, m, runeMsg('?'))
	if !m.showHelp {
		t.Error("? in the result area did not open the help overlay")
	}
}

func TestHistoryIsCapped(t *testing.T) {
	m := newTestModel(stubLookup)
	for i := 0; i < maxHistory+1; i++ {
		m.input.SetValue(fmt.Sprintf("10.0.%d.%d", i/256, i%256))
		// the returned commands are not run, so only the first query is ever
		// dispatched and the submissions stay cheap
		m, _ = updateModel(t, m, keyMsg(tea.KeyEnter))
	}

	if got := len(m.history); got != maxHistory {
		t.Fatalf("history has %d entries, want %d", got, maxHistory)
	}
	if got, want := m.history[0], "10.0.0.1"; got != want {
		t.Errorf("the oldest history entry is %q, want %q", got, want)
	}
	if got, want := m.history[len(m.history)-1], "10.0.1.244"; got != want {
		t.Errorf("the newest history entry is %q, want %q", got, want)
	}

	m, _ = updateModel(t, m, keyMsg(tea.KeyUp))
	if got, want := m.input.Value(), "10.0.1.244"; got != want {
		t.Errorf("↑ recalled %q, want %q", got, want)
	}
}

func TestTinyAndZeroWindow(t *testing.T) {
	t.Run("height 3", func(t *testing.T) {
		m := newTestModel(stubLookup)
		m, _ = updateModel(t, m, tea.WindowSizeMsg{Width: 40, Height: 3})
		if got := m.resultHeight(); got < minResultHeight {
			t.Errorf("resultHeight = %d, want at least %d", got, minResultHeight)
		}
		if view := m.View(); !strings.Contains(view, "输入 IP / IPv6 / 域名") {
			t.Errorf("View() lost the input line:\n%s", view)
		}
	})

	t.Run("zero size", func(t *testing.T) {
		m := newTestModel(stubLookup)
		m, _ = updateModel(t, m, tea.WindowSizeMsg{Width: 0, Height: 0})
		view := m.View()
		if !strings.Contains(view, "输入 IP / IPv6 / 域名") {
			t.Errorf("View() lost the input line:\n%s", view)
		}
		if !strings.Contains(view, "初始化中") {
			t.Errorf("View() does not show the initialising placeholder:\n%s", view)
		}
	})
}

func TestHelpResetsScroll(t *testing.T) {
	m := newTestModel(stubLookup)
	m, _ = updateModel(t, m, tea.WindowSizeMsg{Width: 100, Height: 12})
	for i := 0; i < 8; i++ {
		m = submitQuery(t, m, fmt.Sprintf("10.0.0.%d", i))
	}
	if topLine(m) == 0 {
		t.Fatal("the result area does not follow the newest result")
	}

	m, _ = updateModel(t, m, runeMsg('?'))
	body := visibleBody(m)
	if !strings.Contains(body[0], "ctrl+l") {
		t.Fatalf("the help overlay starts at %q, want the top of the key table", body[0])
	}
	if strings.Contains(body[0], "美国") {
		t.Fatalf("the help overlay starts at the end of the results: %q", body[0])
	}

	m, _ = updateModel(t, m, keyMsg(tea.KeyEsc))
	if m.showHelp {
		t.Fatal("esc did not close the help overlay")
	}
	if !m.stick {
		t.Error("closing the help overlay did not restore the bottom-following behaviour")
	}
	if got, want := topLine(m), m.maxTopFor(len(m.bodyLines())); got != want {
		t.Errorf("top line after closing the help = %d, want %d (the newest result)", got, want)
	}
}

func TestClearResults(t *testing.T) {
	m := newTestModel(stubLookup)
	m = submitQuery(t, m, "8.8.8.8")

	m, _ = updateModel(t, m, keyMsg(tea.KeyCtrlL))
	if len(m.blocks) != 0 {
		t.Errorf("ctrl+l kept %d blocks, want none", len(m.blocks))
	}
	if view := m.View(); strings.Contains(view, "美国 加利福尼亚州") {
		t.Errorf("ctrl+l did not clear the result area:\n%s", view)
	}
	if len(m.history) != 1 {
		t.Errorf("ctrl+l dropped the history, len = %d", len(m.history))
	}
}

func TestClearResultsDropsQueuedQueries(t *testing.T) {
	m := newTestModel(stubLookup)
	for i := 0; i < 3; i++ {
		m.input.SetValue(fmt.Sprintf("10.0.0.%d", i))
		var cmd tea.Cmd
		m, cmd = updateModel(t, m, keyMsg(tea.KeyEnter))
		if (i == 0) != (cmd != nil) {
			t.Fatalf("submit %d returned %v, want a command only for the first query", i, cmd != nil)
		}
	}
	if got, want := m.pending, 3; got != want {
		t.Fatalf("pending = %d, want %d", got, want)
	}

	m, _ = updateModel(t, m, keyMsg(tea.KeyCtrlL))
	if got, want := m.pending, 1; got != want {
		t.Errorf("pending = %d after ctrl+l, want %d (only the in-flight query)", got, want)
	}

	// the answer of the cleared query must not panic and must stop counting
	m, cmd := updateModel(t, m, lookupDoneMsg{seq: 0, line: "10.0.0.0", es: stubEntities("10.0.0.0")})
	if cmd != nil {
		t.Error("a cleared query was dispatched again")
	}
	if got := m.pending; got != 0 {
		t.Errorf("pending = %d after the in-flight answer, want 0", got)
	}
	if got := m.runningSeq; got != -1 {
		t.Errorf("runningSeq = %d, want -1", got)
	}
	if len(m.blocks) != 0 {
		t.Errorf("the cleared answer reappeared, %d blocks", len(m.blocks))
	}
}

func TestFocusToggle(t *testing.T) {
	m := newTestModel(stubLookup)

	m, _ = updateModel(t, m, keyMsg(tea.KeyTab))
	if m.focus != focusResults || m.input.Focused() {
		t.Fatal("tab did not move the focus to the result area")
	}
	m, _ = updateModel(t, m, keyMsg(tea.KeyShiftTab))
	if m.focus != focusInput || !m.input.Focused() {
		t.Fatal("shift+tab did not move the focus back to the input")
	}
}

// topLine is the index of the first visible result line.
func topLine(m *Model) int {
	return m.scrollTop(len(m.bodyLines()))
}

func TestScrolling(t *testing.T) {
	m := newTestModel(stubLookup)
	m, _ = updateModel(t, m, tea.WindowSizeMsg{Width: 100, Height: 10})
	for i := 0; i < 5; i++ {
		m = submitQuery(t, m, fmt.Sprintf("10.0.0.%d", i))
	}
	if topLine(m) == 0 {
		t.Fatal("the view is not pinned to the newest result")
	}

	m, _ = updateModel(t, m, keyMsg(tea.KeyTab))
	bottom := topLine(m)
	m, _ = updateModel(t, m, keyMsg(tea.KeyUp))
	if topLine(m) != bottom-1 {
		t.Fatalf("↑ scrolled to %d, want %d", topLine(m), bottom-1)
	}
	m, _ = updateModel(t, m, keyMsg(tea.KeyPgUp))
	if topLine(m) >= bottom-1 {
		t.Fatalf("pgup did not scroll a page, top = %d", topLine(m))
	}
	m, _ = updateModel(t, m, keyMsg(tea.KeyPgDown))
	m, _ = updateModel(t, m, keyMsg(tea.KeyPgDown))
	if topLine(m) != bottom || !m.stick {
		t.Fatalf("pgdown did not scroll back to the bottom, top = %d", topLine(m))
	}
}

func TestResizeAdjustsResultHeight(t *testing.T) {
	m := newTestModel(stubLookup)

	m, _ = updateModel(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	small := m.resultHeight()
	if got, want := strings.Count(m.View(), "\n")+1, 30; got != want {
		t.Errorf("View() has %d lines, want %d", got, want)
	}

	m, _ = updateModel(t, m, tea.WindowSizeMsg{Width: 100, Height: 50})
	big := m.resultHeight()
	if big <= small {
		t.Fatalf("resultHeight = %d after growing, want more than %d", big, small)
	}
	if got, want := big-small, 20; got != want {
		t.Errorf("resultHeight grew by %d, want %d", got, want)
	}
}

func TestNarrowWindow(t *testing.T) {
	m := newTestModel(stubLookup)
	m, _ = updateModel(t, m, tea.WindowSizeMsg{Width: 20, Height: 10})
	m = submitQuery(t, m, "8.8.8.8")

	view := m.View()
	lines := strings.Split(view, "\n")
	if len(lines) != 10 {
		t.Errorf("View() has %d lines, want 10", len(lines))
	}
	for _, line := range lines {
		if width := lipgloss.Width(line); width > 20 {
			t.Errorf("line %q is %d cells wide, want at most 20", line, width)
		}
	}
}

func TestResultBlocksAreTrimmed(t *testing.T) {
	m := newTestModel(stubLookup)
	for i := 0; i < maxBlocks+5; i++ {
		m.input.SetValue(fmt.Sprintf("10.0.%d.%d", i/256, i%256))
		var cmd tea.Cmd
		m, cmd = updateModel(t, m, keyMsg(tea.KeyEnter))
		if i == 0 && cmd == nil {
			t.Fatal("the first query was not dispatched")
		}
		if i > 0 && cmd != nil {
			t.Fatalf("query %d was dispatched while query 0 was still running", i)
		}
	}

	if len(m.blocks) != maxBlocks {
		t.Fatalf("kept %d blocks, want %d", len(m.blocks), maxBlocks)
	}
	if got := m.blocks[0].seq; got != 5 {
		t.Errorf("the oldest kept block is seq %d, want 5", got)
	}
	if got := m.blocks[len(m.blocks)-1].seq; got != maxBlocks+4 {
		t.Errorf("the newest block is seq %d, want %d", got, maxBlocks+4)
	}
	// the trimmed in-flight query still counts as pending, the trimmed queued
	// ones never run and stop counting
	if got, want := m.pending, maxBlocks+1; got != want {
		t.Errorf("pending = %d, want %d", got, want)
	}

	// the answer of the trimmed query must not panic and must dispatch the next
	// queued query
	m, cmd := updateModel(t, m, lookupDoneMsg{seq: 0, line: "10.0.0.0"})
	if cmd == nil {
		t.Fatal("the next queued query was not dispatched")
	}
	if got, want := m.pending, maxBlocks; got != want {
		t.Errorf("pending = %d after the trimmed answer, want %d", got, want)
	}
	if got, want := m.runningSeq, 5; got != want {
		t.Errorf("runningSeq = %d, want %d", got, want)
	}
}

// TestLookupRunsSerially submits three queries at once and drives the program
// loop by hand, so that a concurrent implementation would be detected: the
// databases must never be queried in parallel.
func TestLookupRunsSerially(t *testing.T) {
	var (
		mu        sync.Mutex
		active    int
		maxActive int
		order     []string
	)
	lookup := func(line string) (entity.Entities, error) {
		mu.Lock()
		active++
		if active > maxActive {
			maxActive = active
		}
		mu.Unlock()

		// leave the window open long enough for an overlapping call to show up
		time.Sleep(5 * time.Millisecond)

		mu.Lock()
		order = append(order, line)
		active--
		mu.Unlock()
		return stubEntities(line), nil
	}

	m := newTestModel(lookup)
	m, _ = updateModel(t, m, tea.WindowSizeMsg{Width: 100, Height: 20})

	msgs := make(chan tea.Msg, 64)
	run := func(cmd tea.Cmd) {
		if cmd == nil {
			return
		}
		go func() {
			for _, msg := range drain(cmd) {
				msgs <- msg
			}
		}()
	}

	want := []string{"10.0.0.1", "10.0.0.2", "10.0.0.3"}
	for _, line := range want {
		m.input.SetValue(line)
		var cmd tea.Cmd
		m, cmd = updateModel(t, m, keyMsg(tea.KeyEnter))
		run(cmd)
	}
	if m.pending != len(want) {
		t.Fatalf("pending = %d, want %d (one running, the rest queued)", m.pending, len(want))
	}

	timeout := time.After(10 * time.Second)
	for m.pending > 0 {
		select {
		case msg := <-msgs:
			var cmd tea.Cmd
			m, cmd = updateModel(t, m, msg)
			run(cmd)
		case <-timeout:
			t.Fatalf("the queue did not drain, pending = %d", m.pending)
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if maxActive > 1 {
		t.Errorf("%d lookups ran at the same time, want at most 1", maxActive)
	}
	if !slices.Equal(order, want) {
		t.Errorf("lookup order = %v, want %v", order, want)
	}
}

// TestHeadlessProgram runs a full program with scripted input, which exercises
// the message loop and the renderer without a terminal.
func TestHeadlessProgram(t *testing.T) {
	var buf bytes.Buffer
	program := tea.NewProgram(
		newTestModel(stubLookup),
		tea.WithInput(strings.NewReader("8.8.8.8\r\x03")),
		tea.WithOutput(&buf),
		tea.WithoutSignalHandler(),
	)

	done := make(chan error, 1)
	go func() {
		_, err := program.Run()
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("program returned %v", err)
		}
	case <-time.After(15 * time.Second):
		program.Kill()
		t.Fatal("the program did not quit on ctrl+c")
	}

	if !strings.Contains(buf.String(), "8.8.8.8") {
		t.Errorf("the rendered output does not contain the query:\n%q", buf.String())
	}
}

// TestHeadlessProgramRendersAnswer feeds the query first and quits only once
// the answer is on screen, so that the asynchronous lookup is covered as well.
func TestHeadlessProgramRendersAnswer(t *testing.T) {
	var out syncBuffer
	in, writer := io.Pipe()
	defer func() { _ = writer.Close() }()

	program := tea.NewProgram(
		newTestModel(stubLookup),
		tea.WithInput(in),
		tea.WithOutput(&out),
		tea.WithoutSignalHandler(),
	)

	done := make(chan error, 1)
	go func() {
		_, err := program.Run()
		done <- err
	}()

	if _, err := io.WriteString(writer, "8.8.8.8\r"); err != nil {
		t.Fatalf("writing the query failed: %v", err)
	}

	deadline := time.After(15 * time.Second)
	for !strings.Contains(out.String(), "美国 加利福尼亚州") {
		select {
		case err := <-done:
			t.Fatalf("program exited early (%v):\n%s", err, out.String())
		case <-deadline:
			program.Kill()
			t.Fatalf("the answer never reached the screen:\n%s", out.String())
		case <-time.After(10 * time.Millisecond):
		}
	}

	if _, err := io.WriteString(writer, "\x03"); err != nil {
		t.Fatalf("writing ctrl+c failed: %v", err)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("program returned %v", err)
		}
	case <-time.After(15 * time.Second):
		program.Kill()
		t.Fatal("the program did not quit on ctrl+c")
	}
}

// syncBuffer is a bytes.Buffer that can be read while the program writes.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
