package czdb

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
)

// TestFetchEntryStopsRetryingWhenCancelled makes sure a cancelled update does not
// sit through the download retries: the first attempt is the last one.
func TestFetchEntryStopsRetryingWhenCancelled(t *testing.T) {
	resetPayloadCache(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		// the caller gives up while the request is in flight
		cancel()
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	// a custom url without the {key} placeholder needs no download key
	_, err := FetchEntryWithContext(ctx, nil, EntryIPv4, []string{srv.URL})
	if err == nil {
		t.Fatal("a cancelled download should fail")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("requests = %d, want 1 (no retries after a cancellation)", got)
	}
}

// TestFetchEntryStopsBeforeRequest covers a context that is already done: no
// request is sent at all.
func TestFetchEntryStopsBeforeRequest(t *testing.T) {
	resetPayloadCache(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
	}))
	defer srv.Close()

	if _, err := FetchEntryWithContext(ctx, nil, EntryIPv4, []string{srv.URL}); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("requests = %d, want 0", got)
	}
}

// TestFetchEntryReportsProgress checks that the progress callback of a czdb
// download carries the announced size and ends with the exact byte count.
func TestFetchEntryReportsProgress(t *testing.T) {
	resetPayloadCache(t)

	v4 := buildFixture(t, 4, 8, columnSelectionAll, ipv4Segments)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(v4.data)))
		_, _ = w.Write(v4.data)
	}))
	defer srv.Close()

	var lastRead, lastTotal int64
	var calls int
	progress := func(read, total int64) {
		calls++
		lastRead, lastTotal = read, total
	}
	got, err := FetchEntryWithContext(context.Background(), progress, EntryIPv4, []string{srv.URL})
	if err != nil {
		t.Fatalf("FetchEntryWithContext: %v", err)
	}
	if len(got) != len(v4.data) {
		t.Fatalf("got %d bytes, want %d", len(got), len(v4.data))
	}
	if calls == 0 {
		t.Fatal("a download must report its progress")
	}
	if lastRead != int64(len(v4.data)) || lastTotal != int64(len(v4.data)) {
		t.Fatalf("final report = (%d, %d), want (%d, %d)", lastRead, lastTotal, len(v4.data), len(v4.data))
	}
}

// TestFetchEntryReusesCachedPayloadWithoutProgress pins the caching semantics a
// front-end has to know about: the second member of the archive is served from
// the cache and reports no progress at all.
func TestFetchEntryReusesCachedPayloadWithoutProgress(t *testing.T) {
	resetPayloadCache(t)

	v4 := buildFixture(t, 4, 8, columnSelectionAll, ipv4Segments)
	v6 := buildFixture(t, 6, 8, columnSelectionAll, ipv6Segments)
	archive := buildZip(t, []struct {
		name string
		data []byte
	}{
		{EntryIPv4, v4.data},
		{EntryIPv6, v6.data},
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(archive)))
		_, _ = w.Write(archive)
	}))
	defer srv.Close()

	var calls int
	progress := func(read, total int64) { calls++ }
	if _, err := FetchEntryWithContext(context.Background(), progress, EntryIPv4, []string{srv.URL}); err != nil {
		t.Fatalf("FetchEntryWithContext(%s): %v", EntryIPv4, err)
	}
	first := calls
	if first == 0 {
		t.Fatal("the first download must report its progress")
	}
	if _, err := FetchEntryWithContext(context.Background(), progress, EntryIPv6, []string{srv.URL}); err != nil {
		t.Fatalf("FetchEntryWithContext(%s): %v", EntryIPv6, err)
	}
	if calls != first {
		t.Fatalf("a cached payload reported %d further progress events", calls-first)
	}
}
