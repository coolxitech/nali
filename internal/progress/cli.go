package progress

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/term"

	"github.com/zu1k/nali/internal/db"
)

const (
	// barWidth is the width of the progress bar the CLI draws.
	barWidth = 20
	// columnGap separates two columns of a rendered line.
	columnGap = "  "
	// messageIndent indents the message lines below a database name.
	messageIndent = "  "
	// defaultWidth is the terminal width assumed when it cannot be measured.
	defaultWidth = 80
)

// CLI renders the events of one update run on a terminal: one line per
// database, redrawn in place, with the same data the TUI panel shows.
type CLI struct {
	w       io.Writer
	width   int
	tracker *Tracker
	state   State

	// printed is the number of lines of the frame on screen; the next frame is
	// drawn over them.
	printed int
	// finished reports whether the end of the run has been rendered.
	finished bool
}

// NewCLI returns a renderer for w, which must be a terminal.
func NewCLI(w io.Writer) *CLI {
	return newCLIWithWidth(w, terminalWidth(w))
}

// newCLIWithWidth returns a renderer for a terminal of a known width.
func newCLIWithWidth(w io.Writer, width int) *CLI {
	if width <= 0 {
		width = defaultWidth
	}
	return &CLI{
		w:       w,
		width:   width,
		tracker: NewTracker(nil),
		state:   StateRunning,
	}
}

// terminalWidth measures w if it is a terminal and falls back to a common
// terminal width otherwise.
func terminalWidth(w io.Writer) int {
	if f, ok := w.(*os.File); ok {
		if width, _, err := term.GetSize(f.Fd()); err == nil && width > 0 {
			return width
		}
	}
	return defaultWidth
}

// Emit renders one event; it is meant to be the emit of db.UpdateDBs.
func (c *CLI) Emit(ev db.UpdateEvent) {
	if ev.Stage == db.StageFinished {
		c.finish()
		return
	}
	if c.finished {
		return
	}
	c.tracker.Apply(ev, time.Now())
	c.draw()
}

// Close renders the end of the run. It is safe to call more than once and after
// a run that never reported StageFinished.
func (c *CLI) Close() {
	c.finish()
}

// finish moves to the finished state, redraws the rows and prints the summary.
func (c *CLI) finish() {
	if c.finished {
		return
	}
	c.finished = true
	c.state = StateFinished
	c.tracker.Finish()
	if c.draw() > 0 {
		// keep the summary off the last database line
		_, _ = io.WriteString(c.w, "\n")
	}
	_, _ = fmt.Fprintln(c.w, c.tracker.Summary())
}

// draw renders the rows and moves the cursor back over the previous frame.
func (c *CLI) draw() int {
	lines := c.lines()
	if len(lines) < c.printed {
		// the frame only grows in practice, but a shorter one must not leave the
		// lines of the previous frame behind
		lines = append(lines, make([]string, c.printed-len(lines))...)
	}
	if len(lines) == 0 {
		return 0
	}

	var b strings.Builder
	if c.printed > 0 {
		fmt.Fprintf(&b, "\x1b[%dA", c.printed)
	}
	for _, line := range lines {
		b.WriteString("\r\x1b[K")
		b.WriteString(line)
		b.WriteString("\n")
	}
	_, _ = io.WriteString(c.w, b.String())

	c.printed = len(lines)
	return len(lines)
}

// lines renders every row of the run, in the order the rows were created.
func (c *CLI) lines() []string {
	rows := c.tracker.Rows()
	l := c.layout(rows)

	lines := make([]string, 0, len(rows))
	for _, row := range rows {
		lines = append(lines, c.rowLines(row, l)...)
	}
	return lines
}

// cliLayout is the width of every column of the output; a width of 0 hides the
// column.
type cliLayout struct {
	name  int
	bar   int
	bytes int
	speed int
	stage int
}

// layout picks the columns that fit into the terminal width. The progress bar is
// the first column a narrow terminal loses, then the speed and finally the byte
// counter, so a line is never wider than the terminal.
func (c *CLI) layout(rows []*Row) cliLayout {
	var l cliLayout
	for _, row := range rows {
		l.name = max(l.name, ansi.StringWidth(row.Name))
		l.stage = max(l.stage, ansi.StringWidth(row.StageText(c.state)))
		l.bytes = max(l.bytes, ansi.StringWidth(row.BytesText()))
		l.speed = max(l.speed, ansi.StringWidth(row.SpeedText()))
	}

	gap := ansi.StringWidth(columnGap)
	// the room left for the bar once the text columns and the spaces between
	// them are placed
	rest := c.width - (l.name + gap + l.stage)
	withSpeed := rest - (gap + l.bytes + gap + l.speed)
	if c.hasBar(rows) && withSpeed-(gap+barWidth+1+percentWidth) >= 0 {
		l.bar = barWidth
		return l
	}
	if withSpeed >= 0 {
		return l
	}
	if rest-(gap+l.bytes) >= 0 {
		l.speed = 0
		return l
	}
	l.speed, l.bytes = 0, 0
	return l
}

// hasBar reports whether any row has a download progress to draw, which is what
// decides whether the output reserves a bar column at all.
func (c *CLI) hasBar(rows []*Row) bool {
	for _, row := range rows {
		if row.ShowsBar() {
			return true
		}
	}
	return false
}

// rowLines renders one row: the columns of the database, followed by the
// indented message lines of a failure.
func (c *CLI) rowLines(row *Row, l cliLayout) []string {
	var b strings.Builder
	b.WriteString(padRight(row.Name, l.name))
	if l.bar > 0 {
		b.WriteString(columnGap)
		if row.ShowsBar() {
			done, rest := row.BarParts(l.bar)
			b.WriteString(strings.Repeat("█", done))
			b.WriteString(strings.Repeat("░", rest))
			b.WriteString(" ")
			b.WriteString(row.PercentText())
		} else {
			// no progress to show: leave the cells blank so the columns of the
			// other rows stay aligned
			b.WriteString(strings.Repeat(" ", l.bar+1+percentWidth))
		}
	}
	if l.bytes > 0 {
		b.WriteString(columnGap)
		b.WriteString(padRight(row.BytesText(), l.bytes))
	}
	if l.speed > 0 {
		b.WriteString(columnGap)
		b.WriteString(padRight(row.SpeedText(), l.speed))
	}
	if l.stage > 0 {
		b.WriteString(columnGap)
		b.WriteString(row.StageText(c.state))
	}

	lines := []string{c.clip(b.String())}
	if row.ShowsMessage() {
		for _, messageLine := range strings.Split(row.Message, "\n") {
			lines = append(lines, c.clip(messageIndent+messageLine))
		}
	}
	return lines
}

// clip keeps a rendered line within the width of the terminal.
func (c *CLI) clip(line string) string {
	line = strings.TrimRight(line, " ")
	if ansi.StringWidth(line) <= c.width {
		return line
	}
	return strings.TrimRight(ansi.Truncate(line, c.width, ""), " ")
}

// padRight pads s with spaces to width cells.
func padRight(s string, width int) string {
	if w := ansi.StringWidth(s); w < width {
		return s + strings.Repeat(" ", width-w)
	}
	return s
}
