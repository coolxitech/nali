package progress

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/zu1k/nali/internal/db"
)

// at returns a fixed point in time so that the speed of a row is measured over
// intervals the test controls.
func at(offset time.Duration) time.Time {
	return time.Unix(1700000000, 0).Add(offset)
}

// failureMessage is the multi-line message the update reports for a failed
// download.
func failureMessage() string {
	return strings.Join([]string{
		"czdb 数据库下载失败，请手动下载解压后保存到本地: ./czdb.xdb ",
		"提示：自动下载需要环境变量 CZDB_DOWNLOAD_KEY",
		"error: 下载失败",
	}, "\n")
}

// TestRowApplyKeepsEarlierFields covers the fields an event only overwrites when
// it carries them.
func TestRowApplyKeepsEarlierFields(t *testing.T) {
	var row Row
	row.Apply(db.UpdateEvent{DB: "czdb", Stage: db.StageDownload, Message: "正在下载", Read: 10, Total: 100}, at(0))
	if !row.Seen {
		t.Error("the row is not marked as seen")
	}

	// an event without message, error and stage only reports progress
	row.Apply(db.UpdateEvent{Stage: db.StageDownload, Read: 20, Total: 100}, at(time.Second))
	if got, want := row.Message, "正在下载"; got != want {
		t.Errorf("message = %q, want %q", got, want)
	}
	if got, want := row.Stage, db.StageDownload; got != want {
		t.Errorf("stage = %q, want %q", got, want)
	}
	if got, want := row.Read, int64(20); got != want {
		t.Errorf("read = %d, want %d", got, want)
	}
}

// TestRowStageText covers every label of the stage column.
func TestRowStageText(t *testing.T) {
	tests := []struct {
		name  string
		row   Row
		state State
		want  string
	}{
		{
			name:  "not reported while running",
			row:   Row{Name: "czdb"},
			state: StateRunning,
			want:  "等待",
		},
		{
			name:  "not reported after the run",
			row:   Row{Name: "czdb"},
			state: StateFinished,
			want:  "未更新",
		},
		{
			name:  "not reported in the confirmation",
			row:   Row{Name: "czdb"},
			state: StateConfirm,
			want:  "",
		},
		{
			name:  "download",
			row:   Row{Seen: true, Stage: db.StageDownload},
			state: StateRunning,
			want:  "下载",
		},
		{
			name:  "verify",
			row:   Row{Seen: true, Stage: db.StageVerify},
			state: StateRunning,
			want:  "校验",
		},
		{
			name:  "save",
			row:   Row{Seen: true, Stage: db.StageSave},
			state: StateRunning,
			want:  "写入",
		},
		{
			name:  "done",
			row:   Row{Seen: true, Stage: db.StageDone, Message: "czdb 数据库下载成功: ./czdb.xdb"},
			state: StateFinished,
			want:  "完成",
		},
		{
			name:  "up to date",
			row:   Row{Seen: true, Stage: db.StageDone, Message: "czdb 数据库已是最新版本: ./czdb.xdb"},
			state: StateFinished,
			want:  "已是最新版本",
		},
		{
			name:  "failed",
			row:   Row{Seen: true, Stage: db.StageDone, Err: errors.New("下载失败"), Message: failureMessage()},
			state: StateFinished,
			want:  "失败",
		},
		{
			name:  "cancelled",
			row:   Row{Seen: true, Stage: db.StageDone, Err: context.Canceled, Message: failureMessage()},
			state: StateRunning,
			want:  "已取消",
		},
		{
			name:  "no stage",
			row:   Row{Seen: true},
			state: StateRunning,
			want:  "",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.row.StageText(test.state); got != test.want {
				t.Errorf("StageText(%v) = %q, want %q", test.state, got, test.want)
			}
		})
	}
}

// TestRowPercentText covers the percentage column, which keeps the leading space
// of its three digits.
func TestRowPercentText(t *testing.T) {
	tests := []struct {
		name string
		row  Row
		want string
	}{
		{name: "unknown size", row: Row{Read: 512}, want: " --%"},
		{name: "zero", row: Row{Total: 100}, want: "  0%"},
		{name: "single digit", row: Row{Read: 6, Total: 100}, want: "  6%"},
		{name: "two digits", row: Row{Read: 63, Total: 100}, want: " 63%"},
		{name: "complete", row: Row{Read: 100, Total: 100}, want: "100%"},
		{name: "over the total", row: Row{Read: 200, Total: 100}, want: "100%"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.row.PercentText(); got != test.want {
				t.Errorf("PercentText() = %q, want %q", got, test.want)
			}
		})
	}
}

// TestRowBytesText covers the byte column: a database of unknown size only shows
// what has arrived, and one that downloaded nothing shows nothing.
func TestRowBytesText(t *testing.T) {
	tests := []struct {
		name string
		row  Row
		want string
	}{
		{name: "nothing yet", row: Row{}, want: ""},
		{name: "unknown total", row: Row{Read: 512}, want: "512 B"},
		{name: "known total", row: Row{Read: 4 * 1024 * 1024, Total: 8 * 1024 * 1024}, want: "4.0 MB / 8.0 MB"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.row.BytesText(); got != test.want {
				t.Errorf("BytesText() = %q, want %q", got, test.want)
			}
		})
	}
}

// TestRowSpeedText covers the speed column.
func TestRowSpeedText(t *testing.T) {
	if got := (&Row{}).SpeedText(); got != "" {
		t.Errorf("SpeedText() of a row without speed = %q, want empty", got)
	}
	if got, want := (&Row{Speed: 3.2 * 1024 * 1024}).SpeedText(), "3.2 MB/s"; got != want {
		t.Errorf("SpeedText() = %q, want %q", got, want)
	}
}

// TestRowShowsBar covers which rows have a progress bar.
func TestRowShowsBar(t *testing.T) {
	if (&Row{}).ShowsBar() {
		t.Error("a row without events draws a bar")
	}
	if !(&Row{Total: 10}).ShowsBar() {
		t.Error("a row with a known size draws no bar")
	}

	var row Row
	row.Apply(db.UpdateEvent{DB: "czdb", Stage: db.StageDownload}, at(0))
	if !row.ShowsBar() {
		t.Error("a row that started downloading draws no bar")
	}
}

// TestRowShowsMessage covers which rows list their message.
func TestRowShowsMessage(t *testing.T) {
	tests := []struct {
		name string
		row  Row
		want bool
	}{
		{name: "no message", row: Row{Seen: true, Stage: db.StageDownload}, want: false},
		{name: "failure", row: Row{Seen: true, Stage: db.StageDone, Err: errors.New("下载失败"), Message: failureMessage()}, want: true},
		{name: "cancelled", row: Row{Seen: true, Stage: db.StageDone, Err: context.Canceled, Message: failureMessage()}, want: false},
		{name: "done without download", row: Row{Seen: true, Stage: db.StageDone, Message: "暂不支持该类型数据库的自动更新"}, want: true},
		{name: "done after a download", row: Row{Seen: true, Stage: db.StageDone, Total: 10, Message: "czdb 数据库下载成功: ./czdb.xdb"}, want: false},
		{name: "running with a message", row: Row{Seen: true, Stage: db.StageDownload, Total: 10, Message: "正在下载"}, want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.row.ShowsMessage(); got != test.want {
				t.Errorf("ShowsMessage() = %v, want %v", got, test.want)
			}
		})
	}
}

// TestRowSpeedNeedsTwoSamples covers the speed: the announcement of a download
// and the first sample are no speed yet, only the second sample is.
func TestRowSpeedNeedsTwoSamples(t *testing.T) {
	var row Row
	// the announcement carries no bytes
	row.Apply(db.UpdateEvent{DB: "czdb", Stage: db.StageDownload, Message: "正在下载最新 czdb 数据库..."}, at(0))
	if row.Speed != 0 {
		t.Errorf("the announcement reported a speed of %v", row.Speed)
	}

	// the first sample is the baseline: the connection setup is not charged to it
	row.Apply(db.UpdateEvent{Stage: db.StageDownload, Read: 1024, Total: 4096}, at(time.Second))
	if got := row.SpeedText(); got != "" {
		t.Errorf("the first sample already shows a speed: %q", got)
	}

	row.Apply(db.UpdateEvent{Stage: db.StageDownload, Read: 2048, Total: 4096}, at(2*time.Second))
	if got, want := row.SpeedText(), "1.0 KB/s"; got != want {
		t.Errorf("SpeedText() = %q, want %q", got, want)
	}
}

// TestRowSpeedKeepsBaselineOnEqualTimestamps covers two events the clock cannot
// tell apart: the next event measures over the longer interval.
func TestRowSpeedKeepsBaselineOnEqualTimestamps(t *testing.T) {
	var row Row
	row.Apply(db.UpdateEvent{Stage: db.StageDownload, Read: 1024, Total: 4096}, at(0))
	row.Apply(db.UpdateEvent{Stage: db.StageDownload, Read: 2048, Total: 4096}, at(0))
	if got := row.SpeedText(); got != "" {
		t.Errorf("two events of the same instant report a speed: %q", got)
	}
	row.Apply(db.UpdateEvent{Stage: db.StageDownload, Read: 3072, Total: 4096}, at(time.Second))
	if got, want := row.SpeedText(), "2.0 KB/s"; got != want {
		t.Errorf("SpeedText() = %q, want %q", got, want)
	}
}

// TestRowSpeedDropsOnRetry covers a download that starts over: the old speed
// would contradict the bytes the row shows.
func TestRowSpeedDropsOnRetry(t *testing.T) {
	var row Row
	row.Apply(db.UpdateEvent{Stage: db.StageDownload, Read: 1024, Total: 4096}, at(0))
	row.Apply(db.UpdateEvent{Stage: db.StageDownload, Read: 2048, Total: 4096}, at(time.Second))
	if got, want := row.SpeedText(), "1.0 KB/s"; got != want {
		t.Fatalf("SpeedText() = %q, want %q", got, want)
	}

	// the retry reports fewer bytes than before
	row.Apply(db.UpdateEvent{Stage: db.StageDownload, Read: 512, Total: 4096}, at(2*time.Second))
	if got := row.SpeedText(); got != "" {
		t.Errorf("the speed of the abandoned attempt is still shown: %q", got)
	}
	if got, want := row.BytesText(), "512 B / 4.0 KB"; got != want {
		t.Errorf("BytesText() = %q, want %q", got, want)
	}

	// the new attempt measures its own speed
	row.Apply(db.UpdateEvent{Stage: db.StageDownload, Read: 1536, Total: 4096}, at(3*time.Second))
	if got, want := row.SpeedText(), "1.0 KB/s"; got != want {
		t.Errorf("SpeedText() = %q, want %q", got, want)
	}
}

// TestRowReadIsClamped covers a server that reports more bytes than it announced.
func TestRowReadIsClamped(t *testing.T) {
	var row Row
	row.Apply(db.UpdateEvent{Stage: db.StageDownload, Read: 200, Total: 100}, at(0))
	if got, want := row.Read, int64(100); got != want {
		t.Errorf("read = %d, want %d", got, want)
	}
	if got, want := row.PercentText(), "100%"; got != want {
		t.Errorf("PercentText() = %q, want %q", got, want)
	}

	// an unknown total is kept as it is, so the row shows the raw bytes
	var unknown Row
	unknown.Apply(db.UpdateEvent{Stage: db.StageDownload, Read: 200}, at(0))
	if got, want := unknown.Read, int64(200); got != want {
		t.Errorf("read = %d, want %d", got, want)
	}
}

// TestRowDoneCoversSpeedAndSize covers the end of a database: it downloads no
// further, and a successful one holds exactly the announced size.
func TestRowDoneCoversSpeedAndSize(t *testing.T) {
	var row Row
	row.Apply(db.UpdateEvent{Stage: db.StageDownload, Read: 1024, Total: 4096}, at(0))
	row.Apply(db.UpdateEvent{Stage: db.StageDownload, Read: 2048, Total: 4096}, at(time.Second))
	if row.Speed <= 0 {
		t.Fatal("the speed was not measured")
	}

	row.Apply(db.UpdateEvent{Stage: db.StageDone, Message: "czdb 数据库下载成功: ./czdb.xdb"}, at(2*time.Second))
	if got := row.SpeedText(); got != "" {
		t.Errorf("a finished database still reports a speed: %q", got)
	}
	if got, want := row.Read, int64(4096); got != want {
		t.Errorf("read = %d, want %d", got, want)
	}

	// a failed database keeps the bytes it received
	var failed Row
	failed.Apply(db.UpdateEvent{Stage: db.StageDownload, Read: 1024, Total: 4096}, at(0))
	failed.Apply(db.UpdateEvent{Stage: db.StageDone, Err: errors.New("下载失败"), Message: failureMessage()}, at(time.Second))
	if got, want := failed.Read, int64(1024); got != want {
		t.Errorf("read of the failed database = %d, want %d", got, want)
	}
}

// TestBarParts covers the cells of the progress bar.
func TestBarParts(t *testing.T) {
	tests := []struct {
		name       string
		row        Row
		width      int
		done, rest int
	}{
		{name: "unknown size", row: Row{}, width: 20, done: 0, rest: 20},
		{name: "empty", row: Row{Total: 100}, width: 20, done: 0, rest: 20},
		{name: "half", row: Row{Read: 50, Total: 100}, width: 20, done: 10, rest: 10},
		{name: "complete", row: Row{Read: 100, Total: 100}, width: 20, done: 20, rest: 0},
		{name: "over the total", row: Row{Read: 200, Total: 100}, width: 20, done: 20, rest: 0},
		{name: "zero width", row: Row{Read: 50, Total: 100}, width: 0, done: 0, rest: 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			done, rest := test.row.BarParts(test.width)
			if done != test.done || rest != test.rest {
				t.Errorf("BarParts(%d) = %d, %d, want %d, %d", test.width, done, rest, test.done, test.rest)
			}
		})
	}
}

// TestHumanBytes covers the sizes the interface shows.
func TestHumanBytes(t *testing.T) {
	tests := []struct {
		n    int64
		want string
	}{
		{n: 0, want: "0 B"},
		{n: 512, want: "512 B"},
		{n: 1024, want: "1.0 KB"},
		{n: 8 * 1024 * 1024, want: "8.0 MB"},
		{n: 3 * 1024 * 1024 * 1024, want: "3.0 GB"},
	}
	for _, test := range tests {
		if got := HumanBytes(test.n); got != test.want {
			t.Errorf("HumanBytes(%d) = %q, want %q", test.n, got, test.want)
		}
	}
}

// TestTrackerKeepsRowOrder covers the stable order of the rows: a database the
// preview did not list is appended, never inserted.
func TestTrackerKeepsRowOrder(t *testing.T) {
	tracker := NewTracker(db.DbNameListForUpdate)
	names := func() []string {
		out := make([]string, 0, len(tracker.Rows()))
		for _, row := range tracker.Rows() {
			out = append(out, row.Name)
		}
		return out
	}

	tracker.Apply(db.UpdateEvent{DB: "cdn", Stage: db.StageDownload, Read: 2, Total: 20}, at(0))
	tracker.Apply(db.UpdateEvent{DB: "czdb", Stage: db.StageDownload, Read: 1, Total: 10}, at(0))
	tracker.Apply(db.UpdateEvent{DB: "czdb", Stage: db.StageVerify}, at(0))
	if got := names(); !slices.Equal(got, db.DbNameListForUpdate) {
		t.Errorf("row order = %v, want %v", got, db.DbNameListForUpdate)
	}
	if row := tracker.Row("czdb"); row.Stage != db.StageVerify || row.Read != 1 {
		t.Errorf("the czdb row holds stage %q and %d bytes, want verify and 1", row.Stage, row.Read)
	}

	// a database the preview did not list is appended
	tracker.Apply(db.UpdateEvent{DB: "ipinfo", Stage: db.StageDownload, Read: 1, Total: 2}, at(0))
	want := append(append([]string{}, db.DbNameListForUpdate...), "ipinfo")
	if got := names(); !slices.Equal(got, want) {
		t.Errorf("row order = %v, want %v", got, want)
	}
}

// TestTrackerApplyFinished covers the event that ends the run: it belongs to no
// row.
func TestTrackerApplyFinished(t *testing.T) {
	tracker := NewTracker(db.DbNameListForUpdate)
	if row := tracker.Apply(db.UpdateEvent{Stage: db.StageFinished}, at(0)); row != nil {
		t.Errorf("StageFinished was stored in the row %q", row.Name)
	}
	if got, want := len(tracker.Rows()), len(db.DbNameListForUpdate); got != want {
		t.Errorf("the tracker has %d rows, want %d", got, want)
	}
}

// TestTrackerFinishCounts covers the four counters of the summary.
func TestTrackerFinishCounts(t *testing.T) {
	tracker := NewTracker([]string{"czdb", "zxipv6wry", "ip2region", "cdn", "ipinfo"})
	// czdb succeeded
	tracker.Apply(db.UpdateEvent{DB: "czdb", Stage: db.StageDownload, Read: 1, Total: 2}, at(0))
	tracker.Apply(db.UpdateEvent{DB: "czdb", Stage: db.StageDone, Message: "czdb 数据库下载成功: ./czdb.xdb"}, at(time.Second))
	// zxipv6wry failed
	tracker.Apply(db.UpdateEvent{DB: "zxipv6wry", Stage: db.StageDone, Err: errors.New("下载失败"), Message: failureMessage()}, at(time.Second))
	// ip2region was cancelled
	tracker.Apply(db.UpdateEvent{DB: "ip2region", Stage: db.StageDone, Err: context.Canceled, Message: failureMessage()}, at(time.Second))
	// cdn reported a stage that is no end, so it is no success either
	tracker.Apply(db.UpdateEvent{DB: "cdn", Stage: db.StageVerify}, at(time.Second))
	// ipinfo was never reported

	tracker.Finish()
	if got, want := tracker.Succeeded(), 1; got != want {
		t.Errorf("Succeeded() = %d, want %d", got, want)
	}
	if got, want := tracker.Failed(), 1; got != want {
		t.Errorf("Failed() = %d, want %d", got, want)
	}
	if got, want := tracker.Cancelled(), 1; got != want {
		t.Errorf("Cancelled() = %d, want %d", got, want)
	}
	if got, want := tracker.Skipped(), 1; got != want {
		t.Errorf("Skipped() = %d, want %d", got, want)
	}
}

// TestTrackerSummary covers the summary line with and without its optional
// parts.
func TestTrackerSummary(t *testing.T) {
	tracker := NewTracker([]string{"czdb", "cdn"})
	tracker.Apply(db.UpdateEvent{DB: "czdb", Stage: db.StageDone, Message: "czdb 数据库下载成功: ./czdb.xdb"}, at(0))
	tracker.Finish()
	if got, want := tracker.Summary(), "成功 1 个 / 失败 0 个 · 跳过 1 个"; got != want {
		t.Errorf("Summary() = %q, want %q", got, want)
	}

	empty := NewTracker([]string{"czdb"})
	empty.Apply(db.UpdateEvent{DB: "czdb", Stage: db.StageDone, Message: "czdb 数据库下载成功: ./czdb.xdb"}, at(0))
	empty.Finish()
	if got, want := empty.Summary(), "成功 1 个 / 失败 0 个"; got != want {
		t.Errorf("Summary() = %q, want %q", got, want)
	}

	all := NewTracker([]string{"czdb", "zxipv6wry", "ip2region", "cdn"})
	all.Apply(db.UpdateEvent{DB: "czdb", Stage: db.StageDone, Message: "czdb 数据库下载成功: ./czdb.xdb"}, at(0))
	all.Apply(db.UpdateEvent{DB: "zxipv6wry", Stage: db.StageDone, Err: errors.New("下载失败"), Message: failureMessage()}, at(0))
	all.Apply(db.UpdateEvent{DB: "ip2region", Stage: db.StageDone, Err: context.Canceled, Message: failureMessage()}, at(0))
	all.Finish()
	want := "成功 1 个 / 失败 1 个 · 已取消 1 个 · 跳过 1 个"
	if got := all.Summary(); got != want {
		t.Errorf("Summary() = %q, want %q", got, want)
	}
	// the hint of the TUI is no part of the shared summary
	if strings.Contains(all.Summary(), "失败原因见上面的错误") {
		t.Errorf("Summary() carries the hint of the panel: %q", all.Summary())
	}
}
