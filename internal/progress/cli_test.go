package progress

import (
	"bytes"
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/zu1k/nali/internal/db"
)

// frame returns the output a renderer wrote since the last call, so that a test
// can look at one redrawn frame at a time.
func frame(buf *bytes.Buffer, from int) string {
	return buf.String()[from:]
}

// lines splits the output into the lines it left on the screen, without the
// escape sequences that clear them.
func lines(out string) []string {
	var result []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimPrefix(line, "\r")
		line = strings.TrimPrefix(line, ansi.EraseLineRight)
		result = append(result, line)
	}
	return result
}

// TestCLIRendersOneLinePerDatabase covers the first frame: the columns of a
// database, with the same data the panel shows.
func TestCLIRendersOneLinePerDatabase(t *testing.T) {
	var buf bytes.Buffer
	c := newCLIWithWidth(&buf, 100)

	c.Emit(db.UpdateEvent{DB: "czdb", Stage: db.StageDownload, Read: 4 * 1024 * 1024, Total: 8 * 1024 * 1024})
	first := buf.String()
	for _, want := range []string{"czdb", "50%", "4.0 MB / 8.0 MB", "下载", "█", "░"} {
		if !strings.Contains(first, want) {
			t.Errorf("the first frame does not contain %q:\n%s", want, first)
		}
	}
	if strings.Contains(first, "成功 ") {
		t.Errorf("the first frame already shows the summary:\n%s", first)
	}
	// one line per database, no redraw yet
	if got, want := strings.Count(first, "\n"), 1; got != want {
		t.Errorf("the first frame has %d newlines, want %d:\n%s", got, want, first)
	}
	if strings.Contains(first, "\x1b[1A") {
		t.Errorf("the first frame moves the cursor up:\n%q", first)
	}

	// the next frame is drawn over the previous one
	from := buf.Len()
	c.Emit(db.UpdateEvent{DB: "czdb", Stage: db.StageVerify})
	second := frame(&buf, from)
	if !strings.Contains(second, "\x1b[1A") {
		t.Errorf("the second frame does not move the cursor up:\n%q", second)
	}
	if !strings.Contains(second, "校验") {
		t.Errorf("the second frame does not show the new stage:\n%q", second)
	}

	// a second database of the run adds a line below the first one
	from = buf.Len()
	c.Emit(db.UpdateEvent{DB: "cdn", Stage: db.StageDownload, Read: 1, Total: 2})
	third := frame(&buf, from)
	if !strings.Contains(third, "\x1b[1A") {
		t.Errorf("the third frame does not move the cursor over the first row:\n%q", third)
	}
	if got, want := strings.Count(third, "\r\x1b[K"), 2; got != want {
		t.Errorf("the third frame draws %d lines, want %d:\n%q", got, want, third)
	}
	if !strings.Contains(third, "czdb") || !strings.Contains(third, "cdn") {
		t.Errorf("the third frame does not keep both databases:\n%q", third)
	}
}

// TestCLIAnnouncementHasNoBytes covers the event that announces a download: it
// carries no bytes, so the row shows no size yet.
func TestCLIAnnouncementHasNoBytes(t *testing.T) {
	var buf bytes.Buffer
	c := newCLIWithWidth(&buf, 100)

	c.Emit(db.UpdateEvent{DB: "zxipv6wry", Stage: db.StageDownload, Message: "正在下载最新 ZX IPv6数据库..."})
	out := buf.String()
	if !strings.Contains(out, "zxipv6wry") || !strings.Contains(out, "下载") {
		t.Errorf("the frame does not show the database:\n%s", out)
	}
	for _, unwanted := range []string{"0 B", "/ 0 B", "0%"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("the frame shows %q for a database without bytes:\n%s", unwanted, out)
		}
	}
	// the announcement is no failure, so its message stays off the screen
	if strings.Contains(out, "正在下载最新") {
		t.Errorf("the frame lists the announcement as a message:\n%s", out)
	}
}

// TestCLIFailureMessage covers the indented message lines below a failed
// database.
func TestCLIFailureMessage(t *testing.T) {
	var buf bytes.Buffer
	c := newCLIWithWidth(&buf, 100)

	c.Emit(db.UpdateEvent{DB: "czdb", Stage: db.StageDone, Err: errors.New("下载失败"), Message: failureMessage()})
	out := buf.String()
	for _, want := range []string{"czdb", "失败", "请手动下载解压后保存到本地", "CZDB_DOWNLOAD_KEY", "error: 下载失败"} {
		if !strings.Contains(out, want) {
			t.Errorf("the output does not contain %q:\n%s", want, out)
		}
	}

	// every message line is indented below the name line
	printed := lines(out)
	for len(printed) > 0 && printed[len(printed)-1] == "" {
		printed = printed[:len(printed)-1]
	}
	if got, want := len(printed), 1+3; got != want {
		t.Fatalf("%d lines are printed, want %d:\n%s", got, want, out)
	}
	if !strings.Contains(printed[0], "czdb") {
		t.Errorf("the first line is no database line: %q", printed[0])
	}
	for _, line := range printed[1:] {
		if !strings.HasPrefix(line, messageIndent) {
			t.Errorf("the message line %q is not indented", line)
		}
	}

	c.Close()
	if summary := buf.String(); !strings.Contains(summary, "成功 0 个 / 失败 1 个") {
		t.Errorf("the summary does not count the failure:\n%s", summary)
	}
}

// TestCLICancelledRow covers a database the user stopped: it is no failure and
// carries no download hint.
func TestCLICancelledRow(t *testing.T) {
	var buf bytes.Buffer
	c := newCLIWithWidth(&buf, 100)

	c.Emit(db.UpdateEvent{DB: "czdb", Stage: db.StageDownload, Read: 512})
	c.Emit(db.UpdateEvent{
		DB:      "czdb",
		Stage:   db.StageDone,
		Err:     context.Canceled,
		Message: failureMessage(),
	})
	out := buf.String()
	if !strings.Contains(out, "已取消") {
		t.Errorf("the row does not say it was cancelled:\n%s", out)
	}
	for _, unwanted := range []string{"请手动下载", "CZDB_DOWNLOAD_KEY", "失败"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("the output still shows %q for a cancelled database:\n%s", unwanted, out)
		}
	}

	c.Close()
	if got := buf.String(); !strings.Contains(got, "已取消 1 个") {
		t.Errorf("the summary does not count the cancelled database:\n%s", got)
	}
}

// TestCLICloseIsIdempotent covers the end of the run: the summary is printed
// once, whether it arrives as StageFinished or as Close.
func TestCLICloseIsIdempotent(t *testing.T) {
	var buf bytes.Buffer
	c := newCLIWithWidth(&buf, 100)

	c.Emit(db.UpdateEvent{DB: "czdb", Stage: db.StageDone, Message: "czdb 数据库已是最新版本: ./czdb.xdb"})
	c.Emit(db.UpdateEvent{Stage: db.StageFinished})
	out := buf.String()
	if !strings.Contains(out, "成功 1 个 / 失败 0 个") {
		t.Errorf("the output does not show the summary:\n%s", out)
	}
	if !strings.HasSuffix(out, "\n") {
		t.Errorf("the output does not end with a newline:\n%q", out)
	}
	// the summary stands on its own line, separated from the database lines
	if !strings.Contains(out, "\n\n成功 1 个 / 失败 0 个\n") {
		t.Errorf("the summary is not separated from the rows:\n%q", out)
	}
	// the row moved to the finished state
	if !strings.Contains(out, "已是最新版本") {
		t.Errorf("the row does not show the up to date stage:\n%s", out)
	}

	// closing again and an event after the end change nothing
	before := buf.Len()
	c.Close()
	c.Emit(db.UpdateEvent{DB: "cdn", Stage: db.StageDownload, Read: 1, Total: 2})
	if after := buf.Len(); after != before {
		t.Errorf("the finished renderer wrote %d more bytes:\n%s", after-before, buf.String()[before:])
	}
}

// TestCLICloseWithoutEvents covers a run that reported nothing: the summary is
// still printed.
func TestCLICloseWithoutEvents(t *testing.T) {
	var buf bytes.Buffer
	c := newCLIWithWidth(&buf, 100)

	c.Close()
	if got, want := buf.String(), "成功 0 个 / 失败 0 个\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

// TestCLINarrowWidth covers the columns a narrow terminal loses: the bar goes
// first, then the speed and the bytes, and no line is ever wider than the
// terminal.
func TestCLINarrowWidth(t *testing.T) {
	widths := []int{60, 40, 20}
	for _, width := range widths {
		t.Run(strconv.Itoa(width), func(t *testing.T) {
			var buf bytes.Buffer
			c := newCLIWithWidth(&buf, width)

			c.Emit(db.UpdateEvent{DB: "zxipv6wry", Stage: db.StageDownload, Read: 4 * 1024 * 1024, Total: 8 * 1024 * 1024})
			c.Emit(db.UpdateEvent{DB: "zxipv6wry", Stage: db.StageDownload, Read: 5 * 1024 * 1024, Total: 8 * 1024 * 1024})
			c.Emit(db.UpdateEvent{
				DB:      "zxipv6wry",
				Stage:   db.StageDone,
				Err:     errors.New("下载失败"),
				Message: failureMessage(),
			})
			c.Close()

			out := buf.String()
			for _, line := range lines(out) {
				if strings.Contains(line, "成功 ") {
					// the summary is not clipped: a cut off count would be useless
					continue
				}
				if got := ansi.StringWidth(line); got > width {
					t.Errorf("the line %q is %d cells wide, want at most %d", line, got, width)
				}
			}
			if width <= 60 && strings.ContainsAny(out, "█░") {
				t.Errorf("a %d cell terminal still draws a progress bar:\n%s", width, out)
			}
			if !strings.Contains(out, "zxipv6wry") {
				t.Errorf("the database name is lost at %d cells:\n%s", width, out)
			}
			if !strings.Contains(out, "成功 0 个 / 失败 1 个") {
				t.Errorf("the summary is lost at %d cells:\n%s", width, out)
			}
		})
	}
}
