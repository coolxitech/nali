package tui

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/zu1k/nali/internal/db"
	"github.com/zu1k/nali/internal/progress"
	"github.com/zu1k/nali/pkg/entity"
)

// stubUpdater stands in for db.UpdateDBs: the test scripts every event, so
// nothing is downloaded and no real database is touched.
type stubUpdater struct {
	events  chan db.UpdateEvent
	started chan struct{}
	once    sync.Once
	// onStart is called by the update goroutine before the first event, so a
	// test can see when the run really began.
	onStart func()

	mu  sync.Mutex
	ctx context.Context
}

func newStubUpdater() *stubUpdater {
	return &stubUpdater{
		events:  make(chan db.UpdateEvent),
		started: make(chan struct{}),
	}
}

// update is the Options.Update of the model under test. It reports exactly the
// events the test sends and returns once they are closed, which closes the
// channel of the model the same way the real update does.
func (s *stubUpdater) update(ctx context.Context, emit func(db.UpdateEvent)) {
	if s.onStart != nil {
		s.onStart()
	}
	s.mu.Lock()
	s.ctx = ctx
	s.mu.Unlock()
	close(s.started)

	for ev := range s.events {
		emit(ev)
	}
}

// context waits until the update has been started and returns its context.
func (s *stubUpdater) context(t *testing.T) context.Context {
	t.Helper()
	select {
	case <-s.started:
	case <-time.After(5 * time.Second):
		t.Fatal("the update was never started")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ctx
}

// stop ends the run: the stub returns and the channel of the model is closed.
func (s *stubUpdater) stop() {
	s.once.Do(func() { close(s.events) })
}

// updateHarness drives the update panel one message at a time, exactly like the
// Bubble Tea loop does, but without a terminal.
type updateHarness struct {
	t    *testing.T
	m    *Model
	stub *stubUpdater
	msgs chan tea.Msg
}

// newUpdateHarness builds a model whose databases are missing, so that a
// Refresh can be seen in the title bar.
func newUpdateHarness(t *testing.T, refresh func() []DatabaseInfo) *updateHarness {
	t.Helper()
	return newUpdateHarnessWith(t, stubLookup, refresh, nil)
}

// newUpdateHarnessWith builds the same model with a lookup and a Reset the test
// controls itself.
func newUpdateHarnessWith(
	t *testing.T,
	lookup func(string) (entity.Entities, error),
	refresh func() []DatabaseInfo,
	reset func(),
) *updateHarness {
	t.Helper()

	stub := newStubUpdater()
	m := New(Options{
		Version: "v9.9.9",
		Lookup:  lookup,
		Databases: []DatabaseInfo{
			{Kind: "IPv4", Names: []string{"czdb"}, Note: "文件缺失，请先执行 nali update"},
		},
		Update:  stub.update,
		Refresh: refresh,
		Reset:   reset,
	})
	_ = m.Init()

	h := &updateHarness{t: t, m: m, stub: stub, msgs: make(chan tea.Msg, 8)}
	t.Cleanup(stub.stop)
	return h
}

// run starts the command in the background, as the program loop does.
func (h *updateHarness) run(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	go func() { h.msgs <- cmd() }()
}

// press sends one key to the model and starts the command it returns.
func (h *updateHarness) press(msg tea.KeyMsg) {
	h.t.Helper()
	var cmd tea.Cmd
	h.m, cmd = updateModel(h.t, h.m, msg)
	h.run(cmd)
}

// await receives the message of the pending command, feeds it to the model and
// starts the command it returns.
func (h *updateHarness) await() {
	h.t.Helper()
	select {
	case msg := <-h.msgs:
		var cmd tea.Cmd
		h.m, cmd = updateModel(h.t, h.m, msg)
		h.run(cmd)
	case <-time.After(5 * time.Second):
		h.t.Fatal("no update message arrived")
	}
}

// deliver reports one event of the update and waits until the model stored it.
func (h *updateHarness) deliver(ev db.UpdateEvent) {
	h.t.Helper()
	select {
	case h.stub.events <- ev:
	case <-time.After(5 * time.Second):
		h.t.Fatal("the stub did not accept the event")
	}
	h.await()
}

// start opens the panel and starts the update.
func (h *updateHarness) start() {
	h.t.Helper()
	h.press(runeMsg('u'))
	if h.m.panel != panelConfirm {
		h.t.Fatalf("u left the panel in state %v, want confirm", h.m.panel)
	}
	h.press(keyMsg(tea.KeyEnter))
	if h.m.panel != panelRunning {
		h.t.Fatalf("enter left the panel in state %v, want running", h.m.panel)
	}
}

// bodyLineContaining returns the visible result line that contains want.
func bodyLineContaining(t *testing.T, m *Model, want string) string {
	t.Helper()
	for _, line := range visibleBody(m) {
		if strings.Contains(line, want) {
			return line
		}
	}
	t.Fatalf("no visible line contains %q:\n%s", want, strings.Join(visibleBody(m), "\n"))
	return ""
}

func TestUpdatePanelOpensWithU(t *testing.T) {
	t.Run("empty input", func(t *testing.T) {
		h := newUpdateHarness(t, nil)

		h.press(runeMsg('u'))
		if h.m.panel != panelConfirm {
			t.Fatalf("state = %v after u, want confirm", h.m.panel)
		}
		view := h.m.View()
		for _, want := range []string{"数据库更新", "czdb", "zxipv6wry", "enter 开始更新 · esc 关闭"} {
			if !strings.Contains(view, want) {
				t.Errorf("View() does not contain %q:\n%s", want, view)
			}
		}
		if got := h.m.input.Value(); got != "" {
			t.Errorf("input = %q, want u to stay out of it", got)
		}
	})

	t.Run("input has text", func(t *testing.T) {
		h := newUpdateHarness(t, nil)
		h.m.input.SetValue("8.8.8.8")

		h.press(runeMsg('u'))
		if h.m.panel != panelClosed {
			t.Fatalf("state = %v, want the panel closed", h.m.panel)
		}
		if got, want := h.m.input.Value(), "8.8.8.8u"; got != want {
			t.Errorf("input = %q, want %q", got, want)
		}
	})

	t.Run("result area focus", func(t *testing.T) {
		h := newUpdateHarness(t, nil)
		h.m.input.SetValue("8.8.8.8")
		h.press(keyMsg(tea.KeyTab))
		if h.m.focus != focusResults {
			t.Fatal("tab did not move the focus to the result area")
		}

		h.press(runeMsg('u'))
		if h.m.panel != panelConfirm {
			t.Fatalf("state = %v, want the panel open", h.m.panel)
		}
		if got, want := h.m.input.Value(), "8.8.8.8"; got != want {
			t.Errorf("input = %q, want it untouched", got)
		}
	})

	t.Run("no update option", func(t *testing.T) {
		m := newTestModel(stubLookup)
		m, _ = updateModel(t, m, runeMsg('u'))
		if m.panel != panelClosed {
			t.Fatalf("state = %v, want the panel hidden without Options.Update", m.panel)
		}
		if got, want := m.input.Value(), "u"; got != want {
			t.Errorf("input = %q, want %q", got, want)
		}
	})
}

func TestUpdatePanelShowsProgress(t *testing.T) {
	h := newUpdateHarness(t, nil)
	now := time.Unix(1700000000, 0)
	h.m.now = func() time.Time { return now }

	h.start()
	if ctx := h.stub.context(t); ctx.Err() != nil {
		t.Fatal("the update was cancelled right after it started")
	}

	const total = 8 * 1024 * 1024
	// the announcement carries no bytes and is not a speed sample, so the speed
	// appears from the second real sample on
	h.deliver(db.UpdateEvent{DB: "czdb", Stage: db.StageDownload, Message: "正在下载最新 czdb 数据库..."})
	now = now.Add(time.Second)
	h.deliver(db.UpdateEvent{DB: "czdb", Stage: db.StageDownload, Read: 2 * 1024 * 1024, Total: total})
	now = now.Add(time.Second)
	h.deliver(db.UpdateEvent{DB: "czdb", Stage: db.StageDownload, Read: 4 * 1024 * 1024, Total: total})

	view := h.m.View()
	for _, want := range []string{"czdb", "50%", "4.0 MB / 8.0 MB", "2.0 MB/s", "下载"} {
		if !strings.Contains(view, want) {
			t.Errorf("View() does not contain %q:\n%s", want, view)
		}
	}
	// the panel fills the result area, but the input stays visible and editable
	if !strings.Contains(view, "输入 IP / IPv6 / 域名") {
		t.Errorf("View() lost the input line:\n%s", view)
	}

	// the bar is as wide as the terminal allows
	line := bodyLineContaining(t, h.m, "czdb")
	if got, want := lipgloss.Width(line), h.m.effectiveWidth(); got != want {
		t.Errorf("the czdb line is %d cells wide, want %d:\n%s", got, want, line)
	}
	if !strings.Contains(line, "█") || !strings.Contains(line, "░") {
		t.Errorf("the czdb line has no progress bar: %q", line)
	}

	// a later stage replaces the label of the same row
	h.deliver(db.UpdateEvent{DB: "czdb", Stage: db.StageVerify})
	if view := h.m.View(); !strings.Contains(bodyLineContaining(t, h.m, "czdb"), "校验") {
		t.Errorf("the row did not move to the verify stage:\n%s", view)
	}
	if got := len(h.m.tracker.Rows()); got != len(db.DbNameListForUpdate) {
		t.Errorf("the panel has %d rows, want %d (one per database)", got, len(db.DbNameListForUpdate))
	}
}

func TestUpdatePanelUnknownTotal(t *testing.T) {
	h := newUpdateHarness(t, nil)
	h.start()

	h.deliver(db.UpdateEvent{DB: "zxipv6wry", Stage: db.StageDownload, Read: 512})

	view := h.m.View()
	for _, unwanted := range []string{"0 B / 0 B", "/ 0 B", "0%"} {
		if strings.Contains(view, unwanted) {
			t.Errorf("View() shows %q for a database of unknown size:\n%s", unwanted, view)
		}
	}
	if !strings.Contains(view, "512 B") {
		t.Errorf("View() does not show the downloaded bytes:\n%s", view)
	}
}

func TestUpdatePanelShowsFailure(t *testing.T) {
	h := newUpdateHarness(t, nil)
	h.start()

	message := strings.Join([]string{
		"czdb 数据库下载失败，请手动下载解压后保存到本地: ./czdb.xdb ",
		"提示：自动下载需要环境变量 CZDB_DOWNLOAD_KEY",
		"error: 下载失败",
	}, "\n")
	h.deliver(db.UpdateEvent{DB: "czdb", Stage: db.StageDone, Err: errors.New("下载失败"), Message: message})

	view := h.m.View()
	for _, want := range []string{"czdb", "失败", "数据库下载失败", "CZDB_DOWNLOAD_KEY", "error: 下载失败"} {
		if !strings.Contains(view, want) {
			t.Errorf("View() does not contain %q:\n%s", want, view)
		}
	}

	// every line of the message is indented below the name line
	for _, want := range []string{"提示：自动下载", "error: 下载失败"} {
		line := bodyLineContaining(t, h.m, want)
		if !strings.HasPrefix(line, updateIndent) {
			t.Errorf("the message line %q is not indented", line)
		}
	}

	// the summary counts the database as failed
	h.deliver(db.UpdateEvent{Stage: db.StageFinished})
	if view := h.m.View(); !strings.Contains(view, "成功 0 个 / 失败 1 个") {
		t.Errorf("View() does not count the failure:\n%s", view)
	}
	if view := h.m.View(); !strings.Contains(view, "失败原因见上面的错误") {
		t.Errorf("View() does not point at the error:\n%s", view)
	}
}

func TestUpdatePanelCancelThenFinish(t *testing.T) {
	refreshed := 0
	resets := 0
	h := newUpdateHarnessWith(t, stubLookup, func() []DatabaseInfo {
		refreshed++
		return []DatabaseInfo{{Kind: "IPv4", Names: []string{"czdb"}, Ready: true}}
	}, func() { resets++ })
	h.start()
	ctx := h.stub.context(t)

	h.press(keyMsg(tea.KeyEsc))
	if ctx.Err() == nil {
		t.Fatal("esc did not cancel the update")
	}
	if h.m.panel != panelRunning {
		t.Fatalf("state = %v after esc, want to stay in running until the run ends", h.m.panel)
	}
	if view := h.m.View(); !strings.Contains(view, "正在取消…") {
		t.Errorf("View() does not report the cancellation:\n%s", view)
	}
	if refreshed != 0 {
		t.Errorf("Refresh was called %d times before the run ended, want 0", refreshed)
	}
	if resets != 1 {
		t.Errorf("Reset was called %d times before the run ended, want 1 (before it started)", resets)
	}

	// a database the user cancelled while it was downloading
	h.deliver(db.UpdateEvent{DB: "czdb", Stage: db.StageDownload, Read: 512})
	h.deliver(db.UpdateEvent{
		DB:      "czdb",
		Stage:   db.StageDone,
		Err:     context.Canceled,
		Message: "czdb 数据库下载失败，请手动下载解压后保存到本地: ./czdb.xdb \nerror: context canceled",
	})

	h.deliver(db.UpdateEvent{Stage: db.StageFinished})
	if h.m.panel != panelFinished {
		t.Fatalf("state = %v after StageFinished, want finished", h.m.panel)
	}
	if refreshed != 1 {
		t.Errorf("Refresh was called %d times, want 1", refreshed)
	}
	// a cancelled run ends with StageFinished as well, so the databases are
	// dropped here too
	if resets != 2 {
		t.Errorf("Reset was called %d times, want 2 (before and after the run)", resets)
	}
	view := h.m.View()
	if !strings.Contains(view, "czdb ✓") {
		t.Errorf("the title bar does not show the refreshed database:\n%s", view)
	}
	// the cancelled database is counted apart from the failures, the ones the
	// update never reached are named as skipped
	if !strings.Contains(view, "成功 0 个 / 失败 0 个 · 已取消 1 个 · 跳过 3 个") {
		t.Errorf("View() does not show the summary of the cancelled run:\n%s", view)
	}
	if !strings.Contains(view, "esc 关闭") {
		t.Errorf("View() does not show the hint of the finished state:\n%s", view)
	}
}

func TestUpdatePanelClosesBackToResults(t *testing.T) {
	h := newUpdateHarness(t, nil)
	h.m = submitQuery(t, h.m, "8.8.8.8")

	h.start()
	h.deliver(db.UpdateEvent{DB: "czdb", Stage: db.StageDone, Message: "czdb 数据库已是最新版本: ./czdb.xdb"})
	h.deliver(db.UpdateEvent{Stage: db.StageFinished})
	if h.m.panel != panelFinished {
		t.Fatalf("state = %v, want finished", h.m.panel)
	}
	if !strings.Contains(bodyLineContaining(t, h.m, "czdb"), "已是最新版本") {
		t.Errorf("View() does not show the up to date stage:\n%s", h.m.View())
	}

	h.press(keyMsg(tea.KeyEsc))
	if h.m.panel != panelClosed {
		t.Fatalf("state = %v after esc, want the panel closed", h.m.panel)
	}
	view := h.m.View()
	if strings.Contains(view, "数据库更新") {
		t.Errorf("the panel is still on screen:\n%s", view)
	}
	if !strings.Contains(view, "美国 加利福尼亚州") {
		t.Errorf("the results did not come back:\n%s", view)
	}
}

func TestUpdatePanelCtrlC(t *testing.T) {
	h := newUpdateHarness(t, nil)
	h.start()
	ctx := h.stub.context(t)

	_, cmd := updateModel(t, h.m, keyMsg(tea.KeyCtrlC))
	assertQuit(t, cmd)
	if ctx.Err() == nil {
		t.Fatal("ctrl+c did not cancel the update")
	}
}

func TestUpdatePanelFinishesWhenTheRunEnds(t *testing.T) {
	// a run that returns without a StageFinished event must not leave the panel
	// in running
	h := newUpdateHarness(t, nil)
	h.start()
	h.stub.stop()
	h.await()

	if h.m.panel != panelFinished {
		t.Fatalf("state = %v, want finished after the run ended", h.m.panel)
	}
	if view := h.m.View(); !strings.Contains(view, "成功 0 个 / 失败 0 个") {
		t.Errorf("View() does not show the summary:\n%s", view)
	}
}

func TestUpdatePanelKeepsRowOrder(t *testing.T) {
	h := newUpdateHarness(t, nil)
	h.start()

	h.deliver(db.UpdateEvent{DB: "czdb", Stage: db.StageDownload, Read: 1, Total: 10})
	h.deliver(db.UpdateEvent{DB: "cdn", Stage: db.StageDownload, Read: 2, Total: 20})
	h.deliver(db.UpdateEvent{DB: "czdb", Stage: db.StageVerify})

	if got := len(h.m.tracker.Rows()); got != len(db.DbNameListForUpdate) {
		t.Fatalf("the panel has %d rows, want %d", got, len(db.DbNameListForUpdate))
	}
	names := make([]string, 0, len(h.m.tracker.Rows()))
	for _, row := range h.m.tracker.Rows() {
		names = append(names, row.Name)
	}
	if !slices.Equal(names, db.DbNameListForUpdate) {
		t.Errorf("row order = %v, want %v", names, db.DbNameListForUpdate)
	}
	if czdb := h.m.tracker.Rows()[0]; czdb.Stage != db.StageVerify || czdb.Read != 1 {
		t.Errorf("the czdb row holds stage %q and %d bytes, want verify and 1", czdb.Stage, czdb.Read)
	}
	// each database owns exactly one line
	if got := strings.Count(strings.Join(visibleBody(h.m), "\n"), "czdb"); got != 1 {
		t.Errorf("czdb appears %d times, want once:\n%s", got, strings.Join(visibleBody(h.m), "\n"))
	}

	// a database the preview did not list is appended, not inserted
	h.deliver(db.UpdateEvent{DB: "ipinfo", Stage: db.StageDownload, Read: 1, Total: 2})
	if got, want := len(h.m.tracker.Rows()), len(db.DbNameListForUpdate)+1; got != want {
		t.Fatalf("the panel has %d rows, want %d", got, want)
	}
	if got := h.m.tracker.Rows()[len(h.m.tracker.Rows())-1].Name; got != "ipinfo" {
		t.Errorf("the last row is %q, want ipinfo", got)
	}
}

func TestUpdatePanelTinyWindow(t *testing.T) {
	sizes := []struct{ width, height int }{
		{30, 6},
		{30, 24},
		{20, 4},
		{80, 6},
		{1, 1},
	}
	for _, size := range sizes {
		t.Run(fmt.Sprintf("%dx%d", size.width, size.height), func(t *testing.T) {
			h := newUpdateHarness(t, nil)
			h.m, _ = updateModel(t, h.m, tea.WindowSizeMsg{Width: size.width, Height: size.height})
			h.start()
			h.deliver(db.UpdateEvent{DB: "czdb", Stage: db.StageDownload, Read: 3, Total: 100})
			h.deliver(db.UpdateEvent{DB: "czdb", Stage: db.StageDone, Err: errors.New("下载失败"), Message: "第一行\n第二行"})
			h.deliver(db.UpdateEvent{Stage: db.StageFinished})

			view := h.m.View()
			lines := strings.Split(view, "\n")
			// the frame is complete as soon as the terminal is tall enough for
			// the fixed lines plus one result line
			if size.height > h.m.fixedLines() {
				if got, want := len(lines), h.m.effectiveHeight(); got != want {
					t.Errorf("View() has %d lines, want %d", got, want)
				}
			}
			for _, line := range lines {
				if width := lipgloss.Width(line); width > size.width {
					t.Errorf("line %q is %d cells wide, want at most %d", line, width, size.width)
				}
			}
		})
	}
}

// callLog records the order in which the hooks of the model run.
type callLog struct {
	mu    sync.Mutex
	calls []string
}

func (l *callLog) add(name string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls = append(l.calls, name)
}

func (l *callLog) list() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.calls...)
}

// TestUpdatePanelResetsBeforeUpdate covers the Windows file lock: an opened
// database keeps its file open, so the databases have to be dropped before the
// update renames the new files into place.
func TestUpdatePanelResetsBeforeUpdate(t *testing.T) {
	var calls callLog
	h := newUpdateHarnessWith(t, stubLookup, nil, func() { calls.add("reset") })
	h.stub.onStart = func() { calls.add("update") }
	h.start()

	// the context is only readable once the stub is running, so both calls have
	// happened by now
	h.stub.context(t)
	if got, want := calls.list(), []string{"reset", "update"}; !slices.Equal(got, want) {
		t.Errorf("call order = %v, want %v", got, want)
	}
}

// TestUpdatePanelResetsAfterFinish covers the second reset: the databases the
// update wrote are opened by the next query.
func TestUpdatePanelResetsAfterFinish(t *testing.T) {
	resets := 0
	h := newUpdateHarnessWith(t, stubLookup, nil, func() { resets++ })
	h.start()

	if resets != 1 {
		t.Errorf("Reset was called %d times when the update started, want 1", resets)
	}
	h.deliver(db.UpdateEvent{Stage: db.StageFinished})
	if resets != 2 {
		t.Errorf("Reset was called %d times after the update finished, want 2", resets)
	}
}

// TestUpdatePanelWaitsForLookupBeforeUpdate starts the update while a query is
// still in flight: the run waits for the queue to drain, so the files it
// replaces are never in use.
func TestUpdatePanelWaitsForLookupBeforeUpdate(t *testing.T) {
	release := make(chan struct{})
	lookup := func(line string) (entity.Entities, error) {
		<-release
		return stubEntities(line), nil
	}
	var calls callLog
	h := newUpdateHarnessWith(t, lookup, nil, func() { calls.add("reset") })
	h.stub.onStart = func() { calls.add("update") }

	// a query is in flight before the panel is even opened
	h.m.input.SetValue("8.8.8.8")
	h.press(keyMsg(tea.KeyEnter))
	if h.m.runningSeq < 0 {
		t.Fatal("the query is not in flight")
	}

	h.press(runeMsg('u'))
	h.press(keyMsg(tea.KeyEnter))
	if h.m.panel != panelConfirm {
		t.Fatalf("state = %v, want to stay in confirm until the queue is idle", h.m.panel)
	}
	if !h.m.pendingStart {
		t.Error("the update did not wait for the query in flight")
	}
	if view := h.m.View(); !strings.Contains(view, "等待当前查询完成…") {
		t.Errorf("View() does not report the wait:\n%s", view)
	}
	if got := calls.list(); len(got) != 0 {
		t.Errorf("the update ran while a lookup was in flight: %v", got)
	}

	// the answer arrives: the queue is idle, so the databases are dropped and
	// the update starts
	close(release)
	h.await()
	h.stub.context(t)
	if got, want := calls.list(), []string{"reset", "update"}; !slices.Equal(got, want) {
		t.Errorf("call order = %v, want %v", got, want)
	}
	if h.m.panel != panelRunning {
		t.Errorf("state = %v after the queue drained, want running", h.m.panel)
	}
	if h.m.pendingStart {
		t.Error("the pending start was not cleared")
	}
	if len(h.m.blocks) != 1 || !h.m.blocks[0].done {
		t.Errorf("the query in flight did not finish: %+v", h.m.blocks)
	}
}

// TestUpdatePanelIgnoresEnterWhileRunning checks that no query can be submitted
// while the update runs: it would keep a database open and the replacement of
// its file would fail on Windows.
func TestUpdatePanelIgnoresEnterWhileRunning(t *testing.T) {
	h := newUpdateHarness(t, nil)
	h.start()

	h.m.input.SetValue("8.8.8.8")
	next, cmd := updateModel(t, h.m, keyMsg(tea.KeyEnter))
	h.m = next
	if cmd != nil {
		t.Error("enter during the update returned a command")
	}
	if got := len(h.m.blocks); got != 0 {
		t.Errorf("enter during the update added %d result blocks", got)
	}
	if got := h.m.pending; got != 0 {
		t.Errorf("pending = %d, want 0", got)
	}
	if got := h.m.runningSeq; got != -1 {
		t.Errorf("runningSeq = %d, want -1", got)
	}
	if got, want := h.m.input.Value(), "8.8.8.8"; got != want {
		t.Errorf("input = %q, want the typed line kept for later", got)
	}
	if view := h.m.View(); !strings.Contains(view, "更新进行中 · esc 取消") {
		t.Errorf("View() does not explain that the update is running:\n%s", view)
	}

	// the input stays editable, so the next query can be typed ahead
	h.m, _ = updateModel(t, h.m, runeMsg('x'))
	if got, want := h.m.input.Value(), "8.8.8.8x"; got != want {
		t.Errorf("input = %q, want %q", got, want)
	}
}

// TestUpdatePanelResetNil checks that a model without a Reset hook does not
// panic, whether the queue is idle or busy.
func TestUpdatePanelResetNil(t *testing.T) {
	t.Run("idle queue", func(t *testing.T) {
		h := newUpdateHarness(t, nil)
		h.start()
		h.deliver(db.UpdateEvent{Stage: db.StageFinished})
		if h.m.panel != panelFinished {
			t.Fatalf("state = %v, want finished", h.m.panel)
		}
	})

	t.Run("lookup in flight", func(t *testing.T) {
		release := make(chan struct{})
		lookup := func(line string) (entity.Entities, error) {
			<-release
			return stubEntities(line), nil
		}
		h := newUpdateHarnessWith(t, lookup, nil, nil)

		h.m.input.SetValue("8.8.8.8")
		h.press(keyMsg(tea.KeyEnter))
		h.press(runeMsg('u'))
		h.press(keyMsg(tea.KeyEnter))
		if h.m.panel != panelConfirm {
			t.Fatalf("state = %v, want confirm while the queue is busy", h.m.panel)
		}

		close(release)
		h.await()
		h.stub.context(t)
		if h.m.panel != panelRunning {
			t.Errorf("state = %v, want running", h.m.panel)
		}
		h.deliver(db.UpdateEvent{Stage: db.StageFinished})
		if h.m.panel != panelFinished {
			t.Errorf("state = %v, want finished", h.m.panel)
		}
	})
}

// TestUpdatePanelShowsCancelledRow checks that a database the user stopped is
// not reported as a failure with the download hint of the database layer.
func TestUpdatePanelShowsCancelledRow(t *testing.T) {
	h := newUpdateHarness(t, nil)
	h.start()

	h.deliver(db.UpdateEvent{DB: "czdb", Stage: db.StageDownload, Read: 512})
	h.deliver(db.UpdateEvent{
		DB:    "czdb",
		Stage: db.StageDone,
		Err:   context.Canceled,
		Message: strings.Join([]string{
			"czdb 数据库下载失败，请手动下载解压后保存到本地: ./czdb.xdb ",
			"提示：自动下载需要环境变量 CZDB_DOWNLOAD_KEY",
			"error: context canceled",
		}, "\n"),
	})

	line := bodyLineContaining(t, h.m, "czdb")
	if !strings.Contains(line, "已取消") {
		t.Errorf("the czdb line does not say it was cancelled: %q", line)
	}
	view := h.m.View()
	for _, unwanted := range []string{"请手动下载", "CZDB_DOWNLOAD_KEY", "失败"} {
		if strings.Contains(view, unwanted) {
			t.Errorf("View() still shows %q for a cancelled database:\n%s", unwanted, view)
		}
	}

	h.deliver(db.UpdateEvent{Stage: db.StageFinished})
	if view := h.m.View(); !strings.Contains(view, "已取消 1 个") {
		t.Errorf("the summary does not count the cancelled database:\n%s", view)
	}
	// the cancelled database is no failure, so there is no error to look at
	if view := h.m.View(); strings.Contains(view, "失败原因见上面的错误") {
		t.Errorf("the summary points at errors that are not there:\n%s", view)
	}
}

// TestUpdatePanelRowsWithoutEvents covers the databases the update never
// reported: they say so instead of looking like a download stuck at zero.
func TestUpdatePanelRowsWithoutEvents(t *testing.T) {
	h := newUpdateHarness(t, nil)
	h.start()

	h.deliver(db.UpdateEvent{DB: "czdb", Stage: db.StageDownload, Read: 5, Total: 10})

	// while the run is going the untouched databases wait
	for _, name := range []string{"zxipv6wry", "ip2region", "cdn"} {
		line := bodyLineContaining(t, h.m, name)
		if !strings.Contains(line, "等待") {
			t.Errorf("the %s line does not say it waits: %q", name, line)
		}
		assertNoBar(t, name, line)
	}

	h.deliver(db.UpdateEvent{Stage: db.StageFinished})
	for _, name := range []string{"zxipv6wry", "ip2region", "cdn"} {
		line := bodyLineContaining(t, h.m, name)
		if !strings.Contains(line, "未更新") {
			t.Errorf("the %s line does not say it was not updated: %q", name, line)
		}
		assertNoBar(t, name, line)
	}
	if view := h.m.View(); !strings.Contains(view, "跳过 3 个") {
		t.Errorf("the summary does not count the skipped databases:\n%s", view)
	}
	// the database that reported progress keeps its bar
	if line := bodyLineContaining(t, h.m, "czdb"); !strings.Contains(line, "█") {
		t.Errorf("the czdb line lost its progress bar: %q", line)
	}
}

// assertNoBar fails when line draws a progress bar or a percentage.
func assertNoBar(t *testing.T, name, line string) {
	t.Helper()
	if strings.ContainsAny(line, "█░") {
		t.Errorf("the %s line draws a progress bar: %q", name, line)
	}
	if strings.Contains(line, "%") {
		t.Errorf("the %s line shows a percentage: %q", name, line)
	}
}

// TestUpdatePanelSpeedNeedsTwoSamples covers the speed: the announcement of a
// download and the first sample are no speed yet, only the second sample is.
func TestUpdatePanelSpeedNeedsTwoSamples(t *testing.T) {
	h := newUpdateHarness(t, nil)
	now := time.Unix(1700000000, 0)
	h.m.now = func() time.Time { return now }
	h.start()

	// the announcement carries no bytes
	h.deliver(db.UpdateEvent{DB: "czdb", Stage: db.StageDownload, Message: "正在下载最新 czdb 数据库..."})
	now = now.Add(time.Second)
	// the first sample is the baseline: the connection setup is not charged to it
	h.deliver(db.UpdateEvent{DB: "czdb", Stage: db.StageDownload, Read: 1024, Total: 4096})
	if line := bodyLineContaining(t, h.m, "czdb"); strings.Contains(line, "/s") {
		t.Errorf("the first sample already shows a speed: %q", line)
	}

	now = now.Add(time.Second)
	h.deliver(db.UpdateEvent{DB: "czdb", Stage: db.StageDownload, Read: 2048, Total: 4096})
	if line := bodyLineContaining(t, h.m, "czdb"); !strings.Contains(line, "1.0 KB/s") {
		t.Errorf("the second sample does not show the speed: %q", line)
	}
}

// TestUpdatePanelSpeedDropsOnRetry covers a download that starts over: the old
// speed would contradict the bytes the row shows.
func TestUpdatePanelSpeedDropsOnRetry(t *testing.T) {
	h := newUpdateHarness(t, nil)
	now := time.Unix(1700000000, 0)
	h.m.now = func() time.Time { return now }
	h.start()

	h.deliver(db.UpdateEvent{DB: "czdb", Stage: db.StageDownload, Read: 1024, Total: 4096})
	now = now.Add(time.Second)
	h.deliver(db.UpdateEvent{DB: "czdb", Stage: db.StageDownload, Read: 2048, Total: 4096})
	if line := bodyLineContaining(t, h.m, "czdb"); !strings.Contains(line, "1.0 KB/s") {
		t.Fatalf("the speed was not measured: %q", line)
	}

	// the retry reports fewer bytes than before
	now = now.Add(time.Second)
	h.deliver(db.UpdateEvent{DB: "czdb", Stage: db.StageDownload, Read: 512, Total: 4096})
	line := bodyLineContaining(t, h.m, "czdb")
	if strings.Contains(line, "/s") {
		t.Errorf("the speed of the abandoned attempt is still shown: %q", line)
	}
	if !strings.Contains(line, "512 B / 4.0 KB") {
		t.Errorf("the row does not show the bytes of the retry: %q", line)
	}

	// the new attempt measures its own speed
	now = now.Add(time.Second)
	h.deliver(db.UpdateEvent{DB: "czdb", Stage: db.StageDownload, Read: 1536, Total: 4096})
	if line := bodyLineContaining(t, h.m, "czdb"); !strings.Contains(line, "1.0 KB/s") {
		t.Errorf("the retry is not measured: %q", line)
	}
}

// TestUpdatePanelIgnoresEnterWhenFinished keeps a query out of a panel that
// covers the result area.
func TestUpdatePanelIgnoresEnterWhenFinished(t *testing.T) {
	h := newUpdateHarness(t, nil)
	h.start()
	h.deliver(db.UpdateEvent{Stage: db.StageFinished})

	h.m.input.SetValue("8.8.8.8")
	next, cmd := updateModel(t, h.m, keyMsg(tea.KeyEnter))
	h.m = next
	if cmd != nil {
		t.Error("enter on the summary returned a command")
	}
	if got := len(h.m.blocks); got != 0 {
		t.Errorf("enter on the summary added %d result blocks", got)
	}
	if got := h.m.pending; got != 0 {
		t.Errorf("pending = %d, want 0", got)
	}
	if got, want := h.m.input.Value(), "8.8.8.8"; got != want {
		t.Errorf("input = %q, want the typed line kept for later", got)
	}

	// closing the panel makes the input usable again
	h.press(keyMsg(tea.KeyEsc))
	if h.m.panel != panelClosed {
		t.Fatalf("state = %v after esc, want the panel closed", h.m.panel)
	}
	h.press(keyMsg(tea.KeyEnter))
	if got := len(h.m.blocks); got != 1 {
		t.Errorf("the query was not submitted after closing the panel, %d blocks", got)
	}
}

// TestUpdatePanelSharesProgressModel guards that the panel renders the shared
// progress model instead of formatting the events on its own. The same script is
// fed to the panel and to a standalone progress.Tracker, and the texts the panel
// draws have to be exactly the ones the shared model returns.
func TestUpdatePanelSharesProgressModel(t *testing.T) {
	script := []db.UpdateEvent{
		{DB: "czdb", Stage: db.StageDownload, Message: "正在下载最新 czdb 数据库..."},
		{DB: "czdb", Stage: db.StageDownload, Read: 1024, Total: 4096},
		{DB: "czdb", Stage: db.StageDownload, Read: 2048, Total: 4096},
		{DB: "zxipv6wry", Stage: db.StageDownload, Read: 512},
		{DB: "ip2region", Stage: db.StageDone, Err: context.Canceled, Message: "ip2region 数据库下载失败，请手动下载解压后保存到本地: ./ip2region.xdb \nerror: context canceled"},
	}

	h := newUpdateHarness(t, nil)
	clock := time.Unix(1700000000, 0)
	h.m.now = func() time.Time { return clock }
	h.start()
	for i, ev := range script {
		if i > 0 {
			clock = clock.Add(time.Second)
		}
		h.deliver(ev)
	}
	h.deliver(db.UpdateEvent{Stage: db.StageFinished})

	// the same script through the shared model alone, with the same clock
	reference := progress.NewTracker(db.DbNameListForUpdate)
	refClock := time.Unix(1700000000, 0)
	for i, ev := range script {
		if i > 0 {
			refClock = refClock.Add(time.Second)
		}
		reference.Apply(ev, refClock)
	}
	reference.Finish()

	if got, want := h.m.tracker.Summary(), reference.Summary(); got != want {
		t.Errorf("Summary() = %q, want %q", got, want)
	}
	counters := [][2]string{
		{"Succeeded", fmt.Sprint(h.m.tracker.Succeeded())},
		{"Failed", fmt.Sprint(h.m.tracker.Failed())},
		{"Cancelled", fmt.Sprint(h.m.tracker.Cancelled())},
		{"Skipped", fmt.Sprint(h.m.tracker.Skipped())},
	}
	wants := [][2]string{
		{"Succeeded", fmt.Sprint(reference.Succeeded())},
		{"Failed", fmt.Sprint(reference.Failed())},
		{"Cancelled", fmt.Sprint(reference.Cancelled())},
		{"Skipped", fmt.Sprint(reference.Skipped())},
	}
	for i := range counters {
		if counters[i][1] != wants[i][1] {
			t.Errorf("%s = %s, want %s", counters[i][0], counters[i][1], wants[i][1])
		}
	}

	if got, want := len(h.m.tracker.Rows()), len(reference.Rows()); got != want {
		t.Fatalf("the panel has %d rows, want %d", got, want)
	}
	for i, want := range reference.Rows() {
		got := h.m.tracker.Rows()[i]
		state := progress.StateFinished
		for _, pair := range []struct {
			label string
			got   string
			want  string
		}{
			{"stage", got.StageText(state), want.StageText(state)},
			{"bytes", got.BytesText(), want.BytesText()},
			{"speed", got.SpeedText(), want.SpeedText()},
			{"percent", got.PercentText(), want.PercentText()},
		} {
			if pair.got != pair.want {
				t.Errorf("row %d %s = %q, want %q", i, pair.label, pair.got, pair.want)
			}
		}
		if got.ShowsBar() != want.ShowsBar() || got.ShowsMessage() != want.ShowsMessage() ||
			got.Cancelled() != want.Cancelled() || got.UpToDate() != want.UpToDate() {
			t.Errorf("row %d flags differ from the shared model", i)
		}

		// the drawn line is made of nothing but the shared model's strings
		line := bodyLineContaining(t, h.m, want.Name)
		if want.StageText(state) != "" && !strings.Contains(line, want.StageText(state)) {
			t.Errorf("row %d does not draw the shared stage text %q: %q", i, want.StageText(state), line)
		}
		if want.BytesText() != "" && !strings.Contains(line, want.BytesText()) {
			t.Errorf("row %d does not draw the shared bytes %q: %q", i, want.BytesText(), line)
		}
		if want.SpeedText() != "" && !strings.Contains(line, want.SpeedText()) {
			t.Errorf("row %d does not draw the shared speed %q: %q", i, want.SpeedText(), line)
		}
		if want.ShowsBar() && !strings.Contains(line, want.PercentText()) {
			t.Errorf("row %d does not draw the shared percentage %q: %q", i, want.PercentText(), line)
		}
		if !want.ShowsBar() && strings.Contains(line, "%") {
			t.Errorf("row %d draws a percentage the shared model did not: %q", i, line)
		}
	}
}
