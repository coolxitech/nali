package db

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/coolxitech/nali/pkg/dbif"
)

// clearCaches drops every cached database and query result by going through the
// production invalidation path, so the tests and the update never drift apart.
func clearCaches() {
	ResetCache()
}

func resetCaches(t *testing.T) {
	t.Helper()
	clearCaches()
	t.Cleanup(clearCaches)
}

func fixture(t *testing.T, name string) string {
	t.Helper()
	path, err := filepath.Abs(filepath.Join("..", "..", "testdata", "mmdb", name))
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func TestFindMultipleDatabases(t *testing.T) {
	resetCaches(t)
	withNameDBMap(t,
		&DB{Name: "test-ipinfo", Format: FormatMMDB, File: fixture(t, "ipinfo-lite-test.mmdb"), Types: TypesIP},
		&DB{Name: "test-geolite", Format: FormatMMDB, File: fixture(t, "geolite2-city-test.mmdb"), Types: TypesIP},
	)
	withViper(t, map[string]string{
		"selected.ipv4": "test-ipinfo, test-geolite",
		"selected.ipv6": "test-geolite",
		"selected.lang": "en",
	})

	results := Find(dbif.TypeIPv4, "81.2.69.160")
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
	if results[0].Source != "ipinfo" || results[0].String() != "" {
		t.Errorf("first result: %s %q", results[0].Source, results[0].String())
	}
	if results[1].Source != "geoip" || results[1].String() != "United Kingdom London" {
		t.Errorf("second result: %s %q", results[1].Source, results[1].String())
	}

	// a single selected database behaves as before
	results = Find(dbif.TypeIPv6, "2001:218::1")
	if len(results) != 1 || results[0].String() != "Japan" {
		t.Fatalf("unexpected IPv6 results %+v", results)
	}

	// when every lookup fails (here: an invalid address) there are no results
	if results := Find(dbif.TypeIPv4, "not-an-ip"); results != nil {
		t.Fatalf("expected no results, got %+v", results)
	}
}

func TestFindKeepsFailedLookups(t *testing.T) {
	resetCaches(t)
	cdnFile := filepath.Join(t.TempDir(), "cdn.yml")
	if err := os.WriteFile(cdnFile, []byte("example.net:\n  name: Example\n"), 0644); err != nil {
		t.Fatal(err)
	}
	withNameDBMap(t,
		// the CDN database cannot answer IP queries: its lookup fails
		&DB{Name: "test-cdn", Format: FormatCDNYml, File: cdnFile, Types: TypesCDN},
		&DB{Name: "test-geolite", Format: FormatMMDB, File: fixture(t, "geolite2-city-test.mmdb"), Types: TypesIP},
	)
	withViper(t, map[string]string{"selected.ipv4": "test-cdn,test-geolite", "selected.lang": "en"})

	results := Find(dbif.TypeIPv4, "81.2.69.160")
	if len(results) != 2 {
		t.Fatalf("got %d results, want one per selected database", len(results))
	}
	if results[0].Source != "cdn" || results[0].Found() || results[0].Text() != "" {
		t.Errorf("failed lookup: %+v", results[0])
	}
	if !results[1].Found() || results[1].Text() != "United Kingdom London" {
		t.Errorf("successful lookup: %+v", results[1])
	}
}

func TestDefaultUpdateListHonorsMultipleSelection(t *testing.T) {
	withNameDBMap(t)
	withViper(t, map[string]string{"selected.ipv4": "czdb, ipinfo", "selected.ipv6": "zx", "selected.cdn": "cdn"})

	found := false
	for _, name := range defaultUpdateList() {
		if name == "ipinfo" {
			found = true
		}
	}
	if !found {
		t.Fatal("ipinfo selected as second IPv4 database should be updated")
	}
}

func TestFindOrErrorSuccess(t *testing.T) {
	resetCaches(t)
	withNameDBMap(t, &DB{Name: "test-geolite", Format: FormatMMDB, File: fixture(t, "geolite2-city-test.mmdb"), Types: TypesIP})
	withViper(t, map[string]string{"selected.ipv4": "test-geolite", "selected.lang": "en"})

	results, err := FindOrError(dbif.TypeIPv4, "81.2.69.160")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 || results[0].Source != "geoip" || results[0].Text() != "United Kingdom London" {
		t.Fatalf("unexpected results %+v", results)
	}
}

func TestFindOrErrorUnknownDatabase(t *testing.T) {
	resetCaches(t)
	withNameDBMap(t, &DB{Name: "test-geolite", Format: FormatMMDB, File: fixture(t, "geolite2-city-test.mmdb"), Types: TypesIP})
	withViper(t, map[string]string{"selected.ipv4": "missing-db", "selected.lang": "en"})

	results, err := FindOrError(dbif.TypeIPv4, "81.2.69.160")
	if err == nil {
		t.Fatalf("expected an error for an unknown database, got %+v", results)
	}
	if results != nil {
		t.Errorf("got results %+v alongside the error", results)
	}
	if want := "DB with name missing-db not found!"; err.Error() != want {
		t.Errorf("error = %q, want %q", err, want)
	}
	// the failure is not cached, so the next call reports it again
	if _, err := FindOrError(dbif.TypeIPv4, "81.2.69.160"); err == nil {
		t.Error("the second call should report the same failure")
	}
}

func TestFindOrErrorUnopenableDatabase(t *testing.T) {
	resetCaches(t)
	withNameDBMap(t, &DB{Name: "test-broken", Format: FormatMMDB, File: filepath.Join(t.TempDir(), "missing.mmdb"), Types: TypesIP})
	withViper(t, map[string]string{"selected.ipv4": "test-broken", "selected.lang": "en"})

	results, err := FindOrError(dbif.TypeIPv4, "81.2.69.160")
	if err == nil {
		t.Fatalf("expected an error for an unopenable database, got %+v", results)
	}
	if results != nil {
		t.Errorf("got results %+v alongside the error", results)
	}
	// nothing about the failed database is cached
	if db, found := cachedDB("test-broken"); found {
		t.Errorf("a database that failed to open must not be cached: %v", db)
	}
	if dbs, found := cachedDBs(dbif.TypeIPv4); found {
		t.Errorf("a failed resolution must not be cached by query type: %v", dbs)
	}
	if _, found := queryCache.Load("81.2.69.160"); found {
		t.Error("a failed resolution must not fill the query cache")
	}
}

func TestResolveDBsUnsupportedType(t *testing.T) {
	resetCaches(t)
	if dbs, err := resolveDBs(dbif.QueryType(99)); err == nil {
		t.Fatalf("expected an error for an unsupported query type, got %v", dbs)
	}
}

// TestResolveDBsTagsLookupFailure locks the message GetDBs prints for a bad
// database name: it has always been reported without the "Database init
// failed:" prefix used for databases that cannot be opened.
func TestResolveDBsTagsLookupFailure(t *testing.T) {
	resetCaches(t)
	withNameDBMap(t)
	withViper(t, map[string]string{"selected.ipv4": "missing-db", "selected.lang": "en"})

	_, err := resolveDBs(dbif.TypeIPv4)
	if err == nil {
		t.Fatal("expected an error for an unknown database")
	}
	if !errors.Is(err, errLookupFailed) {
		t.Errorf("a lookup failure must be tagged as such, got %v", err)
	}
	if want := "DB with name missing-db not found!"; err.Error() != want {
		t.Errorf("error = %q, want %q", err, want)
	}
}

// TestFindOrErrorCorruptZXIPv6Database covers the failure that used to kill the
// TUI: a damaged zxipv6wry file must be reported as an error instead of
// terminating the process.
func TestFindOrErrorCorruptZXIPv6Database(t *testing.T) {
	resetCaches(t)
	broken := filepath.Join(t.TempDir(), "zxipv6wry.db")
	if err := os.WriteFile(broken, []byte("this is not a ZX IPv6 database"), 0644); err != nil {
		t.Fatal(err)
	}
	withNameDBMap(t, &DB{Name: "test-zx", Format: FormatZXIPv6Wry, File: broken, Types: TypesIPv6})
	withViper(t, map[string]string{"selected.ipv6": "test-zx", "selected.lang": "zh-CN"})

	results, err := FindOrError(dbif.TypeIPv6, "2001:218::1")
	if err == nil {
		t.Fatalf("expected an error for a corrupt database, got %+v", results)
	}
	if results != nil {
		t.Errorf("got results %+v alongside the error", results)
	}
	for _, want := range []string{"请重新下载", broken} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
	// the failed database is not cached, so a later query reports it again
	if _, found := cachedDB("test-zx"); found {
		t.Error("a database that failed to open must not be cached")
	}
	if _, found := cachedDBs(dbif.TypeIPv6); found {
		t.Error("a failed resolution must not be cached by query type")
	}
}

// TestFindOrErrorUnknownFormat covers a mistyped "format" in config.yaml: it
// must be reported as an error, not panic.
func TestFindOrErrorUnknownFormat(t *testing.T) {
	resetCaches(t)
	withNameDBMap(t, &DB{Name: "test-bogus", Format: Format("bogus"), File: filepath.Join(t.TempDir(), "bogus.db"), Types: TypesIP})
	withViper(t, map[string]string{"selected.ipv4": "test-bogus", "selected.lang": "en"})

	results, err := FindOrError(dbif.TypeIPv4, "81.2.69.160")
	if err == nil {
		t.Fatalf("expected an error for an unknown format, got %+v", results)
	}
	if results != nil {
		t.Errorf("got results %+v alongside the error", results)
	}
	if want := `DB format "bogus" not supported`; !strings.Contains(err.Error(), want) {
		t.Errorf("error = %q, want it to contain %q", err, want)
	}
	if _, found := cachedDB("test-bogus"); found {
		t.Error("a database that failed to open must not be cached")
	}
}

// TestLookupName pins the resolver front-ends use to report a bad database name
// themselves: it resolves names and aliases and keeps the CLI wording.
func TestLookupName(t *testing.T) {
	withNameDBMap(t, &DB{
		Name:      "test-ipinfo",
		NameAlias: []string{"test-alias"},
		Format:    FormatMMDB,
		File:      fixture(t, "ipinfo-lite-test.mmdb"),
		Types:     TypesIP,
	})

	for _, name := range []string{"test-ipinfo", "test-alias"} {
		db, err := LookupName(name)
		if err != nil {
			t.Fatalf("LookupName(%q): %v", name, err)
		}
		if db == nil || db.Name != "test-ipinfo" {
			t.Errorf("LookupName(%q) = %+v, want the test-ipinfo definition", name, db)
		}
	}

	// the retired qqwry database keeps its dedicated hint
	if _, err := LookupName("qqwry"); err == nil || !strings.Contains(err.Error(), "CZDB_KEY") {
		t.Errorf("LookupName(qqwry) error = %v, want the czdb hint", err)
	}

	// unknown names keep the message the CLI prints
	if _, err := LookupName("missing-db"); err == nil || err.Error() != "DB with name missing-db not found!" {
		t.Errorf("LookupName(missing-db) error = %v", err)
	}
}

// TestFindOrErrorConcurrent runs queries from several goroutines at once, the
// way the TUI submits them: resolveDBs and getOrError must not write their
// caches concurrently, and every goroutine must still see the expected answer.
// Every phase starts from cold caches and releases the goroutines together, so
// the writes that fill the caches really do overlap.
func TestFindOrErrorConcurrent(t *testing.T) {
	resetCaches(t)

	cdnFile := filepath.Join(t.TempDir(), "cdn.yml")
	if err := os.WriteFile(cdnFile, []byte("cloudflare.net:\n  name: Cloudflare\n"), 0644); err != nil {
		t.Fatal(err)
	}
	withNameDBMap(t,
		&DB{Name: "test-ipinfo", Format: FormatMMDB, File: fixture(t, "ipinfo-lite-test.mmdb"), Types: TypesIP},
		&DB{Name: "test-cdn", Format: FormatCDNYml, File: cdnFile, Types: TypesCDN},
	)
	withViper(t, map[string]string{
		"selected.ipv4": "test-ipinfo",
		"selected.ipv6": "test-ipinfo",
		"selected.cdn":  "test-cdn",
		"selected.lang": "en",
	})

	queries := []struct {
		typ   dbif.QueryType
		query string
		want  string
	}{
		{dbif.TypeIPv4, "8.8.8.8", "United States AS15169 Google LLC"},
		{dbif.TypeIPv6, "2001:4860:4860::8888", "United States AS15169 Google LLC"},
		// NAT64 addresses are looked up as their IPv4 counterpart
		{dbif.TypeIPv6, "64:ff9b::808:808", "United States AS15169 Google LLC"},
		{dbif.TypeDomain, "a.b.cloudflare.net", "Cloudflare"},
		// no database answers, but the lookup itself succeeds
		{dbif.TypeIPv4, "192.0.2.1", ""},
	}

	const goroutines, rounds, phases = 8, 10, 50
	for phase := 0; phase < phases; phase++ {
		if phase > 0 {
			clearCaches()
		}
		start := make(chan struct{})
		var wg sync.WaitGroup
		for g := 0; g < goroutines; g++ {
			wg.Add(1)
			go func(g int) {
				defer wg.Done()
				<-start
				for i := 0; i < rounds; i++ {
					q := queries[(g+i)%len(queries)]
					results, err := FindOrError(q.typ, q.query)
					if err != nil {
						t.Errorf("goroutine %d: FindOrError(%d, %q): %v", g, q.typ, q.query, err)
						return
					}
					if len(results) != 1 {
						t.Errorf("goroutine %d: FindOrError(%q) returned %d results, want 1", g, q.query, len(results))
						return
					}
					if got := results[0].Text(); got != q.want {
						t.Errorf("goroutine %d: FindOrError(%q) = %q, want %q", g, q.query, got, q.want)
						return
					}
				}
			}(g)
		}
		close(start)
		wg.Wait()
	}

	// the databases were opened once and cached for later queries
	if db, found := cachedDB("test-ipinfo"); !found || db == nil {
		t.Error("the IP database should be cached after the concurrent queries")
	}
	for _, typ := range []dbif.QueryType{dbif.TypeIPv4, dbif.TypeIPv6, dbif.TypeDomain} {
		if dbs, found := cachedDBs(typ); !found || len(dbs) != 1 {
			t.Errorf("query type %d: cached databases = %v, want one", typ, dbs)
		}
	}
}
