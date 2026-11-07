// Package progress holds the progress model of a database update. The update
// command renders it on a terminal and the TUI panel shows it on the alternate
// screen, so both report the same data with the same wording.
package progress

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/coolxitech/nali/internal/db"
)

// State is the phase of one update run.
type State int

const (
	// StateConfirm lists the databases the run is going to refresh and waits for
	// the user to start it.
	StateConfirm State = iota
	// StateRunning shows the progress of the running update.
	StateRunning
	// StateFinished shows the summary of a finished or cancelled update.
	StateFinished
)

// percentWidth is the room the percentage next to the progress bar needs.
const percentWidth = 4

// Row is one database of an update run. A row is created for every database of
// the preview and for every database the update reports, and it is updated in
// place, so the order of the lines never changes.
type Row struct {
	Name    string
	Stage   db.UpdateStage
	Read    int64
	Total   int64
	Speed   float64 // bytes per second, 0 when unknown
	Message string
	Err     error
	// Seen reports whether the update said anything about this database yet. A
	// database the update skipped keeps its row, so the panel shows that it was
	// not touched instead of pretending it is stuck at zero.
	Seen bool

	// downloaded reports whether the row ever reported download progress; only
	// such a row has a progress bar to draw.
	downloaded bool

	// sampled reports whether a download event already provided the baseline for
	// the speed of the next one; lastRead and lastAt are that baseline.
	sampled  bool
	lastRead int64
	lastAt   time.Time
}

// Apply stores one event in the row.
func (r *Row) Apply(ev db.UpdateEvent, now time.Time) {
	r.Seen = true
	if ev.Message != "" {
		r.Message = ev.Message
	}
	if ev.Err != nil {
		r.Err = ev.Err
	}
	if ev.Stage != "" {
		r.Stage = ev.Stage
	}

	switch ev.Stage {
	case db.StageDownload:
		r.downloaded = true
		if ev.Total > 0 {
			r.Total = ev.Total
		}
		r.Read = ev.Read
		if r.Total > 0 && r.Read > r.Total {
			r.Read = r.Total
		}
		// The events are already throttled by the update, so the speed is the
		// difference between two of them and needs no timer of its own. The
		// announcement of a download carries no bytes yet and is no sample:
		// using it as the baseline would charge the connection setup (DNS,
		// TTFB) to the first speed and report a nonsense number.
		if ev.Read <= 0 {
			break
		}
		switch {
		case !r.sampled:
			r.lastRead, r.lastAt, r.sampled = ev.Read, now, true
		case now.Sub(r.lastAt).Seconds() <= 0:
			// the clock cannot tell these two events apart: keep the baseline so
			// the next event measures over a longer interval
		case ev.Read < r.lastRead:
			// a retry started over, so the old speed describes an abandoned
			// attempt and would contradict the bytes the row shows
			r.Speed = 0
			r.lastRead, r.lastAt = ev.Read, now
		default:
			r.Speed = float64(ev.Read-r.lastRead) / now.Sub(r.lastAt).Seconds()
			r.lastRead, r.lastAt = ev.Read, now
		}
	case db.StageDone:
		// a finished database does not download any further, and a successful
		// one holds exactly the announced size
		r.Speed = 0
		if r.Err == nil && r.Total > 0 {
			r.Read = r.Total
		}
	}
}

// Cancelled reports whether the database was stopped by the user cancelling the
// update. The update reports that as an ordinary failure whose error is the
// cancelled context, but the output must not tell the user to download anything
// by hand because of it.
func (r *Row) Cancelled() bool {
	return r.Err != nil && errors.Is(r.Err, context.Canceled)
}

// UpToDate reports whether the database was already current, which the update
// reports as a successful StageDone whose message says so.
func (r *Row) UpToDate() bool {
	return r.Err == nil && r.Stage == db.StageDone && strings.Contains(r.Message, "已是最新版本")
}

// ShowsBar reports whether the row has a download progress to draw. A database
// the update has not reported yet, and one that never downloaded anything, get
// no bar: an empty bar at "--%" looks like a download stuck at zero.
func (r *Row) ShowsBar() bool {
	return r.downloaded || r.Total > 0
}

// ShowsMessage reports whether the row lists the message of its last event below
// the name line. Failures always do, because they are what the user has to act
// on; a database that finished without downloading anything does too, because
// its stage alone would not say what happened. A cancelled database does not:
// its message is the download hint of a failure, which does not apply.
func (r *Row) ShowsMessage() bool {
	if r.Message == "" || r.Cancelled() {
		return false
	}
	if r.Err != nil {
		return true
	}
	return r.Stage == db.StageDone && r.Read == 0 && r.Total == 0
}

// StageText is the label of the stage the row is in. A row the update has not
// reported yet says so instead of looking stuck at zero.
func (r *Row) StageText(s State) string {
	if !r.Seen {
		switch s {
		case StateRunning:
			return "等待"
		case StateFinished:
			return "未更新"
		default:
			// the confirmation state only lists the databases
			return ""
		}
	}
	if r.Cancelled() {
		return "已取消"
	}
	switch r.Stage {
	case db.StageDownload:
		return "下载"
	case db.StageVerify:
		return "校验"
	case db.StageSave:
		return "写入"
	case db.StageDone:
		switch {
		case r.Err != nil:
			return "失败"
		case r.UpToDate():
			return "已是最新版本"
		default:
			return "完成"
		}
	default:
		return ""
	}
}

// BytesText is the "downloaded / total" column. A database whose size the server
// did not announce only shows what has arrived so far, and one that never
// downloaded anything shows nothing at all instead of a misleading "0 B".
func (r *Row) BytesText() string {
	if r.Total <= 0 {
		if r.Read <= 0 {
			return ""
		}
		return HumanBytes(r.Read)
	}
	return HumanBytes(r.Read) + " / " + HumanBytes(r.Total)
}

// SpeedText is the download speed column.
func (r *Row) SpeedText() string {
	if r.Speed <= 0 {
		return ""
	}
	return HumanBytes(int64(r.Speed)) + "/s"
}

// PercentText is the percentage next to the progress bar. A database of unknown
// size has no percentage to show.
func (r *Row) PercentText() string {
	if r.Total <= 0 {
		return " --%"
	}
	percent := r.Read * 100 / r.Total
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}
	return fmt.Sprintf("%3d%%", percent)
}

// BarParts returns how many cells of a bar of width cells are filled and how
// many are left empty.
func (r *Row) BarParts(width int) (done, rest int) {
	if width < 0 {
		width = 0
	}
	if r.Total > 0 {
		done = int(r.Read * int64(width) / r.Total)
		if done < 0 {
			done = 0
		}
		if done > width {
			done = width
		}
	}
	return done, width - done
}

// HumanBytes formats n the way the interface shows sizes.
func HumanBytes(n int64) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	value := float64(n)
	for _, unit := range []string{"KB", "MB", "GB", "TB"} {
		value /= 1024
		if value < 1024 {
			return fmt.Sprintf("%.1f %s", value, unit)
		}
	}
	return fmt.Sprintf("%.1f PB", value/1024)
}

// Tracker aggregates the events of one run. It keeps the rows in the order they
// were created, so the lines of an output never change place.
type Tracker struct {
	rows []*Row

	succeeded int
	failed    int
	cancelled int
	skipped   int
}

// NewTracker returns a tracker whose rows are the databases of preview, the list
// the confirmation state shows.
func NewTracker(preview []string) *Tracker {
	t := &Tracker{}
	for _, name := range preview {
		t.Row(name)
	}
	return t
}

// Rows returns the rows of the run in their stable order.
func (t *Tracker) Rows() []*Row {
	return t.rows
}

// Row returns the row of name, adding one at the end for a database the preview
// did not list. An empty name has no row: it is the whole run, not a database.
func (t *Tracker) Row(name string) *Row {
	if name == "" {
		return nil
	}
	for _, row := range t.rows {
		if row.Name == name {
			return row
		}
	}
	row := &Row{Name: name}
	t.rows = append(t.rows, row)
	return row
}

// Apply stores one event and returns the row it belongs to. The end of the run
// belongs to no row and returns nil.
func (t *Tracker) Apply(ev db.UpdateEvent, now time.Time) *Row {
	row := t.Row(ev.DB)
	if row == nil {
		return nil
	}
	row.Apply(ev, now)
	return row
}

// Finish counts the databases of the run. Cancelled databases are counted apart
// from the failures, and the databases the update skipped are counted too, so
// that the numbers add up to the rows.
func (t *Tracker) Finish() {
	t.succeeded, t.failed, t.cancelled, t.skipped = 0, 0, 0, 0
	for _, row := range t.rows {
		if !row.Seen {
			// the update never reported this database, so it was skipped
			t.skipped++
			continue
		}
		switch {
		case row.Cancelled():
			t.cancelled++
		case row.Stage == db.StageDone && row.Err == nil:
			t.succeeded++
		case row.Err != nil:
			t.failed++
		}
	}
}

// Succeeded is the number of databases that finished without an error.
func (t *Tracker) Succeeded() int { return t.succeeded }

// Failed is the number of databases whose update reported an error that is not
// a cancellation.
func (t *Tracker) Failed() int { return t.failed }

// Cancelled is the number of databases the user stopped.
func (t *Tracker) Cancelled() int { return t.cancelled }

// Skipped is the number of databases the update never reported.
func (t *Tracker) Skipped() int { return t.skipped }

// Summary is the line that counts the databases of the finished run.
func (t *Tracker) Summary() string {
	summary := fmt.Sprintf("成功 %d 个 / 失败 %d 个", t.succeeded, t.failed)
	if t.cancelled > 0 {
		summary += fmt.Sprintf(" · 已取消 %d 个", t.cancelled)
	}
	if t.skipped > 0 {
		summary += fmt.Sprintf(" · 跳过 %d 个", t.skipped)
	}
	return summary
}
