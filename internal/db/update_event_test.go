package db

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// cdnYmlBody builds a valid cdn database of at least size bytes, so a test can
// download a body large enough to be reported in several progress events while
// still passing cdn.CheckFile.
func cdnYmlBody(size int) []byte {
	var b bytes.Buffer
	for i := 0; b.Len() < size; i++ {
		fmt.Fprintf(&b, "host%d.example.com:\n  name: CDN %d\n", i, i)
	}
	return b.Bytes()
}

// collect appends every event to the returned slice.
func collect(events *[]UpdateEvent) func(UpdateEvent) {
	return func(ev UpdateEvent) { *events = append(*events, ev) }
}

// stagesOf returns the stage of every event.
func stagesOf(events []UpdateEvent) []UpdateStage {
	stages := make([]UpdateStage, 0, len(events))
	for _, ev := range events {
		stages = append(stages, ev.Stage)
	}
	return stages
}

// TestUpdateDBsReportsEvents covers the whole event stream of one successful
// update: the start message, the throttled progress reports, validation, saving
// and the final event of the run.
func TestUpdateDBsReportsEvents(t *testing.T) {
	body := cdnYmlBody(512 << 10)
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		_, _ = w.Write(body)
	})
	file := filepath.Join(t.TempDir(), "cdn.yml")
	withNameDBMap(t, &DB{Name: "test-cdn", Format: FormatCDNYml, File: file, DownloadUrls: []string{srv.URL + "/cdn.yml"}})

	var events []UpdateEvent
	failed := UpdateDBs(context.Background(), []string{"test-cdn"}, collect(&events))
	if len(failed) != 0 {
		t.Fatalf("failed databases = %v, want none", failed)
	}

	if len(events) < 5 {
		t.Fatalf("got %d events, want at least download, verify, save, done and finished: %v", len(events), stagesOf(events))
	}

	// The run starts with the message the CLI logs before the download and ends
	// with the single event of the whole run.
	first := events[0]
	if first.Stage != StageDownload || first.DB != "test-cdn" || first.Message != "正在下载最新 test-cdn 数据库..." {
		t.Errorf("first event = %+v, want the download start message", first)
	}
	if first.Read != 0 || first.Total != 0 {
		t.Errorf("first event reports (%d, %d), want (0, 0)", first.Read, first.Total)
	}
	last := events[len(events)-1]
	if last.Stage != StageFinished || last.DB != "" {
		t.Errorf("last event = %+v, want StageFinished without a database", last)
	}

	// Every database event belongs to the database, the run event does not.
	for _, ev := range events[:len(events)-1] {
		if ev.DB != "test-cdn" {
			t.Errorf("event %+v does not name the database", ev)
		}
	}

	// download (1..N) -> verify -> save -> done -> finished
	stages := stagesOf(events)
	if stages[len(stages)-4] != StageVerify || stages[len(stages)-3] != StageSave ||
		stages[len(stages)-2] != StageDone || stages[len(stages)-1] != StageFinished {
		t.Fatalf("stages = %v, want the run to end with verify, save, done, finished", stages)
	}
	downloads := 0
	for _, stage := range stages[:len(stages)-4] {
		if stage != StageDownload {
			t.Fatalf("stages = %v, want only download events before the verify stage", stages)
		}
		downloads++
	}
	if downloads < 2 {
		t.Fatalf("got %d download events, want several (the throttle must not swallow them)", downloads)
	}

	// The progress reports carry the announced size, never go back and end with
	// the exact byte count.
	var progress []UpdateEvent
	for _, ev := range events {
		// the start message is a download event without a byte count
		if ev.Stage == StageDownload && ev.Message == "" {
			progress = append(progress, ev)
		}
	}
	if len(progress) < 2 {
		t.Fatalf("got %d progress reports, want several", len(progress))
	}
	var lastRead int64
	for i, ev := range progress {
		if ev.Total != int64(len(body)) {
			t.Errorf("download event %d: total = %d, want %d", i, ev.Total, len(body))
		}
		if ev.Read < lastRead {
			t.Errorf("download event %d: read went back from %d to %d", i, lastRead, ev.Read)
		}
		lastRead = ev.Read
	}
	if lastRead != int64(len(body)) {
		t.Errorf("last download event reports %d bytes, want %d", lastRead, len(body))
	}

	done := events[len(events)-2]
	if want := fmt.Sprintf("test-cdn 数据库下载成功: %s", file); done.Message != want {
		t.Errorf("done message = %q, want %q", done.Message, want)
	}
	if done.Err != nil {
		t.Errorf("done event of a successful update carries %v", done.Err)
	}

	// the downloaded database is stored as-is
	if data, err := os.ReadFile(file); err != nil || !bytes.Equal(data, body) {
		t.Fatalf("stored database = %d bytes, %v; want %d bytes", len(data), err, len(body))
	}
}

// TestUpdateDBsSkipsUnchangedDatabase pins the shorter event stream of a database
// that is already up to date: the payload is still downloaded and validated, but
// nothing is saved.
func TestUpdateDBsSkipsUnchangedDatabase(t *testing.T) {
	body := cdnYmlBody(1024)
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		_, _ = w.Write(body)
	})
	file := filepath.Join(t.TempDir(), "cdn.yml")
	if err := os.WriteFile(file, body, 0644); err != nil {
		t.Fatal(err)
	}
	withNameDBMap(t, &DB{Name: "test-cdn", Format: FormatCDNYml, File: file, DownloadUrls: []string{srv.URL + "/cdn.yml"}})

	var events []UpdateEvent
	if failed := UpdateDBs(context.Background(), []string{"test-cdn"}, collect(&events)); len(failed) != 0 {
		t.Fatalf("failed databases = %v, want none", failed)
	}

	for _, ev := range events {
		if ev.Stage == StageSave {
			t.Fatalf("an unchanged database must not be saved: %v", stagesOf(events))
		}
	}
	done := events[len(events)-2]
	if done.Stage != StageDone {
		t.Fatalf("event before finished = %+v, want done", done)
	}
	if want := fmt.Sprintf("test-cdn 数据库已是最新版本: %s", file); done.Message != want {
		t.Errorf("done message = %q, want %q", done.Message, want)
	}
	verified := false
	for _, ev := range events {
		if ev.Stage == StageVerify {
			verified = true
		}
	}
	if !verified {
		t.Errorf("an unchanged database is still validated: %v", stagesOf(events))
	}
	if events[len(events)-1].Stage != StageFinished {
		t.Errorf("the run must end with StageFinished: %v", stagesOf(events))
	}
}

// TestUpdateDBsStopsWhenCancelled cancels a running update and checks that the
// download stops, the database is reported as failed with the cancellation and
// the file on disk is untouched.
func TestUpdateDBsStopsWhenCancelled(t *testing.T) {
	const chunk = 32 << 10
	const chunks = 8
	body := cdnYmlBody(chunk * chunks)
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		flusher := w.(http.Flusher)
		for start := 0; start < len(body); start += chunk {
			if _, err := w.Write(body[start : start+chunk]); err != nil {
				return
			}
			flusher.Flush()
			select {
			case <-r.Context().Done():
				return
			case <-time.After(100 * time.Millisecond):
			}
		}
	})

	file := filepath.Join(t.TempDir(), "cdn.yml")
	original := cdnYmlBody(64)
	if err := os.WriteFile(file, original, 0644); err != nil {
		t.Fatal(err)
	}
	withNameDBMap(t, &DB{Name: "test-cdn", Format: FormatCDNYml, File: file, DownloadUrls: []string{srv.URL + "/cdn.yml"}})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var events []UpdateEvent
	failed := UpdateDBs(ctx, []string{"test-cdn"}, func(ev UpdateEvent) {
		events = append(events, ev)
		// give up as soon as bytes arrive
		if ev.Stage == StageDownload && ev.Read > 0 {
			cancel()
		}
	})

	if len(failed) != 1 || failed[0] != "test-cdn" {
		t.Fatalf("failed databases = %v, want [test-cdn]", failed)
	}
	var done *UpdateEvent
	for i := range events {
		if events[i].Stage == StageDone {
			done = &events[i]
		}
	}
	if done == nil {
		t.Fatalf("no done event in %v", stagesOf(events))
	}
	if !errors.Is(done.Err, context.Canceled) {
		t.Fatalf("done event error = %v, want context.Canceled", done.Err)
	}
	if events[len(events)-1].Stage != StageFinished {
		t.Errorf("the run must end with StageFinished: %v", stagesOf(events))
	}

	if data, err := os.ReadFile(file); err != nil || !bytes.Equal(data, original) {
		t.Fatalf("a cancelled update changed the database file: %d bytes, %v", len(data), err)
	}
}

// TestUpdateDBLogsTheSameLines pins the log output of the CLI: UpdateDB is
// UpdateDBs with an emitter that logs the message of every event, so the lines
// (including their trailing spaces and order) must be exactly the ones the
// update has always printed.
func TestUpdateDBLogsTheSameLines(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, serverURL, file string)
		body  string
		want  []string
	}{
		{
			name: "downloaded",
			body: "example.com:\n  name: Example\n",
			want: []string{
				"正在下载最新 test-cdn 数据库...",
				"test-cdn 数据库下载成功: {file}",
			},
		},
		{
			name: "already up to date",
			body: "example.com:\n  name: Example\n",
			setup: func(t *testing.T, serverURL, file string) {
				if err := os.WriteFile(file, []byte("example.com:\n  name: Example\n"), 0644); err != nil {
					t.Fatal(err)
				}
			},
			want: []string{
				"正在下载最新 test-cdn 数据库...",
				"test-cdn 数据库已是最新版本: {file}",
			},
		},
		{
			name: "invalid payload",
			body: "<html>rate limited</html>",
			want: []string{
				"正在下载最新 test-cdn 数据库...",
				// the line has always ended with a space before its newline
				"test-cdn 数据库下载失败，请手动下载解压后保存到本地: {file} ",
				"下载链接： [{url}]",
				"error: 数据库内容出错",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(tt.body))
			})
			file := filepath.Join(t.TempDir(), "cdn.yml")
			if tt.setup != nil {
				tt.setup(t, srv.URL, file)
			}
			withNameDBMap(t, &DB{Name: "test-cdn", Format: FormatCDNYml, File: file, DownloadUrls: []string{srv.URL + "/cdn.yml"}})

			got := captureLog(t, func() { UpdateDB("test-cdn") })

			var want strings.Builder
			for _, line := range tt.want {
				line = strings.ReplaceAll(line, "{file}", file)
				line = strings.ReplaceAll(line, "{url}", srv.URL+"/cdn.yml")
				want.WriteString(line + "\n")
			}
			if got != want.String() {
				t.Fatalf("log output = %q, want %q", got, want.String())
			}
		})
	}
}

// TestUpdateDBsWithoutEmitter covers a front-end that only wants the list of
// failed databases: a nil emitter must be accepted.
func TestUpdateDBsWithoutEmitter(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("example.com:\n  name: Example\n"))
	})
	file := filepath.Join(t.TempDir(), "cdn.yml")
	withNameDBMap(t, &DB{Name: "test-cdn", Format: FormatCDNYml, File: file, DownloadUrls: []string{srv.URL + "/cdn.yml"}})

	if failed := UpdateDBs(context.Background(), []string{"test-cdn"}, nil); len(failed) != 0 {
		t.Fatalf("failed databases = %v, want none", failed)
	}
	if _, err := os.Stat(file); err != nil {
		t.Fatalf("the database was not downloaded: %v", err)
	}
}

// TestUpdateDBsReportsEveryUnsupportedDatabase pins the deduplication key of the
// databases without an automatic updater: they are keyed by name, so two of them
// in one run both report. A timestamp key would collide within one clock tick
// (Windows) and silently drop the second database.
func TestUpdateDBsReportsEveryUnsupportedDatabase(t *testing.T) {
	dir := t.TempDir()
	withNameDBMap(t,
		&DB{Name: "test-ipip", Format: FormatIPIP, File: filepath.Join(dir, "ipip.ipdb")},
		&DB{Name: "test-ip2location", Format: FormatIP2Location, File: filepath.Join(dir, "ip2location.bin")},
	)

	var events []UpdateEvent
	if failed := UpdateDBs(context.Background(), []string{"test-ipip", "test-ip2location"}, collect(&events)); len(failed) != 0 {
		t.Fatalf("failed databases = %v, want none", failed)
	}

	var done []string
	for _, ev := range events {
		if ev.Stage == StageDone {
			done = append(done, ev.DB)
		}
	}
	if len(done) != 2 || done[0] != "test-ipip" || done[1] != "test-ip2location" {
		t.Fatalf("done events = %v, want one per database", done)
	}
	if events[len(events)-1].Stage != StageFinished {
		t.Errorf("the run must end with StageFinished: %v", stagesOf(events))
	}
}

// TestUpdateDBLogsUnsupportedFormat pins the two lines a database without a
// download source and without an internal updater has always produced.
func TestUpdateDBLogsUnsupportedFormat(t *testing.T) {
	withNameDBMap(t, &DB{Name: "test-ipip", Format: FormatIPIP, File: filepath.Join(t.TempDir(), "ipip.db")})

	got := captureLog(t, func() { UpdateDB("test-ipip") })
	want := "暂不支持该类型数据库的自动更新\n可通过指定数据库的 download-urls 从特定链接下载数据库文件\n"
	if got != want {
		t.Fatalf("log output = %q, want %q", got, want)
	}
}

// captureLog runs f and returns everything it logged, without timestamps.
func captureLog(t *testing.T, f func()) string {
	t.Helper()
	var buf bytes.Buffer
	flags := log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	defer func() {
		log.SetOutput(os.Stderr)
		log.SetFlags(flags)
	}()
	f()
	return buf.String()
}
