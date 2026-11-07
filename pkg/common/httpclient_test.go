package common

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// progressRecorder collects the DownloadProgress callbacks of a download.
type progressRecorder struct {
	calls []struct{ read, total int64 }
}

func (p *progressRecorder) report(read, total int64) {
	p.calls = append(p.calls, struct{ read, total int64 }{read, total})
}

func (p *progressRecorder) last() (int64, int64) {
	if len(p.calls) == 0 {
		return 0, 0
	}
	call := p.calls[len(p.calls)-1]
	return call.read, call.total
}

// handlerErr records a failure of a test server handler. The handler runs in its
// own goroutine, where the test may not call t.Fatal, so it stores the error and
// the test body reports it once the request is done.
type handlerErr struct {
	mu  sync.Mutex
	err error
}

func (h *handlerErr) set(err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.err == nil {
		h.err = err
	}
}

func (h *handlerErr) check(t *testing.T) {
	t.Helper()
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.err != nil {
		t.Fatalf("test server: %v", h.err)
	}
}

// slowBody is a response body that is written in several chunks with a pause in
// between, so a download of it can be interrupted halfway. It returns an error
// instead of failing the test, because it runs in the handler goroutine.
func slowBody(r *http.Request, w http.ResponseWriter, body []byte, chunk int, pause time.Duration) error {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return errors.New("the test server does not support flushing")
	}
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	for start := 0; start < len(body); start += chunk {
		end := start + chunk
		if end > len(body) {
			end = len(body)
		}
		if _, err := w.Write(body[start:end]); err != nil {
			return nil
		}
		flusher.Flush()
		if pause <= 0 {
			continue
		}
		select {
		case <-r.Context().Done():
			return nil
		case <-time.After(pause):
		}
	}
	return nil
}

func TestGetWithContextReportsProgress(t *testing.T) {
	// more than progressStep bytes, so the throttle must emit several updates
	body := bytes.Repeat([]byte("0123456789abcdef"), 1<<16)
	var handlerErrs handlerErr
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerErrs.set(slowBody(r, w, body, 64<<10, 0))
	}))
	defer srv.Close()

	var progress progressRecorder
	got, err := GetHttpClient().GetWithContext(context.Background(), progress.report, srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("downloaded %d bytes, want %d", len(got), len(body))
	}

	if len(progress.calls) < 2 {
		t.Fatalf("got %d progress reports, want several", len(progress.calls))
	}
	var lastRead int64
	for i, call := range progress.calls {
		if call.total != int64(len(body)) {
			t.Errorf("report %d: total = %d, want %d", i, call.total, len(body))
		}
		if call.read < lastRead {
			t.Errorf("report %d: read went back from %d to %d", i, lastRead, call.read)
		}
		if call.read > int64(len(body)) {
			t.Errorf("report %d: read = %d, more than the body", i, call.read)
		}
		lastRead = call.read
	}
	if read, total := progress.last(); read != int64(len(body)) || total != int64(len(body)) {
		t.Fatalf("final report = (%d, %d), want (%d, %d)", read, total, len(body), len(body))
	}
	handlerErrs.check(t)
}

// TestGetWithContextReportsFinalValueForSmallBody makes sure a download that is
// too small (or too fast) to be reported while it runs still ends with the exact
// size.
func TestGetWithContextReportsFinalValueForSmallBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "5")
		_, _ = w.Write([]byte("hello"))
	}))
	defer srv.Close()

	var progress progressRecorder
	if _, err := GetHttpClient().GetWithContext(context.Background(), progress.report, srv.URL); err != nil {
		t.Fatal(err)
	}
	if len(progress.calls) == 0 {
		t.Fatal("a download must report its final size")
	}
	if read, total := progress.last(); read != 5 || total != 5 {
		t.Fatalf("final report = (%d, %d), want (5, 5)", read, total)
	}
}

func TestGetWithContextUnknownTotal(t *testing.T) {
	// flushing without a Content-Length makes the server answer with chunked
	// encoding, so the size of the body is not announced
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("chunked body"))
		w.(http.Flusher).Flush()
	}))
	defer srv.Close()

	var progress progressRecorder
	got, err := GetHttpClient().GetWithContext(context.Background(), progress.report, srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "chunked body" {
		t.Fatalf("got %q", got)
	}
	if len(progress.calls) == 0 {
		t.Fatal("a download must report its progress")
	}
	for i, call := range progress.calls {
		if call.total != 0 {
			t.Errorf("report %d: total = %d, want 0 for an unknown size", i, call.total)
		}
	}
	if read, _ := progress.last(); read != int64(len(got)) {
		t.Errorf("final read = %d, want %d", read, len(got))
	}
}

func TestGetWithContextCancelled(t *testing.T) {
	body := bytes.Repeat([]byte("x"), 16<<10)
	var handlerErrs handlerErr
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerErrs.set(slowBody(r, w, body, 4<<10, 20*time.Millisecond))
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var progress progressRecorder
	report := func(read, total int64) {
		progress.report(read, total)
		if read > 0 {
			cancel()
		}
	}
	got, err := GetHttpClient().GetWithContext(ctx, report, srv.URL)
	if err == nil {
		t.Fatalf("a cancelled download should fail, got %d bytes", len(got))
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	handlerErrs.check(t)
}

// TestGetWithContextCancelledBeforeRequest makes sure a cancelled download does
// not reach the network at all.
func TestGetWithContextCancelledBeforeRequest(t *testing.T) {
	var requests atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := GetHttpClient().GetWithContext(ctx, nil, srv.URL); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("a cancelled download sent %d requests", got)
	}
}
