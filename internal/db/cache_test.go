package db

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/coolxitech/nali/pkg/dbif"
)

// closeSpy is a cached database that remembers whether ResetCache closed it.
type closeSpy struct {
	dbif.DB
	closed bool
}

func (c *closeSpy) Close() error {
	c.closed = true
	return nil
}

// TestResetCacheClosesDatabases covers the type assertion ResetCache uses to
// release the databases it drops: dbif.DB has no Close, but the databases that
// keep a file handle or the whole file in memory do.
func TestResetCacheClosesDatabases(t *testing.T) {
	resetCaches(t)

	spy := &closeSpy{}
	if _, err := loadDB("test-spy", func() (dbif.DB, error) { return spy, nil }); err != nil {
		t.Fatal(err)
	}
	if _, found := cachedDB("test-spy"); !found {
		t.Fatal("the database should be cached")
	}

	ResetCache()

	if !spy.closed {
		t.Error("ResetCache must close the databases it drops")
	}
	if _, found := cachedDB("test-spy"); found {
		t.Error("the closed database is still cached")
	}
}

// TestResetCacheReopensDatabases fills the caches with an IP database and a CDN
// database, drops them and checks that the next lookup reopens the files and
// answers as before.
func TestResetCacheReopensDatabases(t *testing.T) {
	resetCaches(t)

	cdnFile := filepath.Join(t.TempDir(), "cdn.yml")
	if err := os.WriteFile(cdnFile, []byte("example.net:\n  name: Example\n"), 0644); err != nil {
		t.Fatal(err)
	}
	withNameDBMap(t,
		&DB{Name: "test-ipinfo", Format: FormatMMDB, File: fixture(t, "ipinfo-lite-test.mmdb"), Types: TypesIP},
		&DB{Name: "test-cdn", Format: FormatCDNYml, File: cdnFile, Types: TypesCDN},
	)
	withViper(t, map[string]string{
		"selected.ipv4": "test-ipinfo",
		"selected.cdn":  "test-cdn",
		"selected.lang": "en",
	})

	const (
		ipQuery     = "8.8.8.8"
		domainQuery = "a.example.net"
		ipAnswer    = "United States AS15169 Google LLC"
		cdnAnswer   = "Example"
	)
	lookup := func(typ dbif.QueryType, query, want string) {
		t.Helper()
		results, err := FindOrError(typ, query)
		if err != nil {
			t.Fatalf("FindOrError(%d, %q): %v", typ, query, err)
		}
		if len(results) != 1 || results[0].Text() != want {
			t.Fatalf("FindOrError(%d, %q) = %+v, want %q", typ, query, results, want)
		}
	}

	lookup(dbif.TypeIPv4, ipQuery, ipAnswer)
	lookup(dbif.TypeDomain, domainQuery, cdnAnswer)

	// the opened databases and the answers are cached
	for _, name := range []string{"test-ipinfo", "test-cdn"} {
		if _, found := cachedDB(name); !found {
			t.Errorf("%s should be cached", name)
		}
	}
	for _, typ := range []dbif.QueryType{dbif.TypeIPv4, dbif.TypeDomain} {
		if _, found := cachedDBs(typ); !found {
			t.Errorf("query type %d should be cached", typ)
		}
	}
	for _, query := range []string{ipQuery, domainQuery} {
		if _, found := queryCache.Load(query); !found {
			t.Errorf("%q should be cached", query)
		}
	}

	ResetCache()

	for _, name := range []string{"test-ipinfo", "test-cdn"} {
		if _, found := cachedDB(name); found {
			t.Errorf("%s is still cached after ResetCache", name)
		}
	}
	for _, typ := range []dbif.QueryType{dbif.TypeIPv4, dbif.TypeDomain} {
		if _, found := cachedDBs(typ); found {
			t.Errorf("query type %d is still cached after ResetCache", typ)
		}
	}
	for _, query := range []string{ipQuery, domainQuery} {
		if _, found := queryCache.Load(query); found {
			t.Errorf("%q is still cached after ResetCache", query)
		}
	}

	// the next lookup reopens both files and answers as before
	lookup(dbif.TypeIPv4, ipQuery, ipAnswer)
	lookup(dbif.TypeDomain, domainQuery, cdnAnswer)
	if _, found := cachedDB("test-ipinfo"); !found {
		t.Error("a lookup after ResetCache should cache the reopened database")
	}
}

// TestDefaultDatabaseIsCachedAndReset covers the built-in database used when no
// database is selected for a type. It must be opened through the name cache:
// opening it directly would keep its file handle out of ResetCache, and a
// database update could then not replace the file (on Windows renaming it fails
// with "Access is denied").
func TestDefaultDatabaseIsCachedAndReset(t *testing.T) {
	resetCaches(t)

	// The default IPv4 database is called "czdb"; this test registers an MMDB
	// fixture under that name so it does not have to build a CZDB file. The copy
	// is what the handle is opened on, so the test can prove it was released.
	file := filepath.Join(t.TempDir(), "default-db.mmdb")
	data, err := os.ReadFile(fixture(t, "ipinfo-lite-test.mmdb"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, data, 0644); err != nil {
		t.Fatal(err)
	}
	withNameDBMap(t, &DB{Name: "czdb", Format: FormatMMDB, File: file, Types: TypesIPv4})
	// a blank selection falls back to the built-in database
	withViper(t, map[string]string{"selected.ipv4": " ", "selected.lang": "zh-CN"})

	const (
		query = "8.8.8.8"
		want  = "United States AS15169 Google LLC"
	)
	lookup := func() {
		t.Helper()
		results, err := FindOrError(dbif.TypeIPv4, query)
		if err != nil {
			t.Fatalf("FindOrError(%q): %v", query, err)
		}
		if len(results) != 1 || results[0].Text() != want {
			t.Fatalf("FindOrError(%q) = %+v, want %q", query, results, want)
		}
	}

	lookup()

	opened, found := cachedDB("czdb")
	if !found {
		t.Fatal("the default database must be cached by name, otherwise ResetCache cannot close it")
	}
	if _, found := cachedDBs(dbif.TypeIPv4); !found {
		t.Error("the resolved databases should be cached by query type")
	}

	ResetCache()

	if _, found := cachedDB("czdb"); found {
		t.Error("the default database is still cached after ResetCache")
	}
	if _, found := cachedDBs(dbif.TypeIPv4); found {
		t.Error("the databases of the query type are still cached after ResetCache")
	}
	// a closed MMDB reader refuses further lookups, so this proves that
	// ResetCache closed the handle it dropped
	if _, err := opened.Find(query); err == nil {
		t.Error("ResetCache did not close the default database")
	}
	if runtime.GOOS == "windows" {
		// Windows refuses to rename a file that is still open or mapped, which is
		// exactly what makes an unreleased handle break a database update
		if err := os.Rename(file, file+".moved"); err != nil {
			t.Fatalf("the database file is still held open after ResetCache: %v", err)
		}
		if err := os.Rename(file+".moved", file); err != nil {
			t.Fatal(err)
		}
	}

	// the next lookup reopens the file and caches the new handle
	lookup()
	if _, found := cachedDB("czdb"); !found {
		t.Error("a lookup after ResetCache should cache the reopened database")
	}
}

// TestResetCacheReadsReplacedFile pins what ResetCache is for: a database
// replaced on disk is only picked up once the cache is dropped, because the
// opened instance answers from the file it read when it was opened.
func TestResetCacheReadsReplacedFile(t *testing.T) {
	resetCaches(t)

	cdnFile := filepath.Join(t.TempDir(), "cdn.yml")
	writeCDN := func(name string) {
		t.Helper()
		if err := os.WriteFile(cdnFile, []byte("example.net:\n  name: "+name+"\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	withNameDBMap(t, &DB{Name: "test-cdn", Format: FormatCDNYml, File: cdnFile, Types: TypesCDN})
	withViper(t, map[string]string{"selected.cdn": "test-cdn"})

	const query = "a.example.net"
	find := func(want string) {
		t.Helper()
		results, err := FindOrError(dbif.TypeDomain, query)
		if err != nil {
			t.Fatalf("FindOrError(%q): %v", query, err)
		}
		if len(results) != 1 || results[0].Text() != want {
			t.Fatalf("FindOrError(%q) = %+v, want %q", query, results, want)
		}
	}

	writeCDN("A")
	find("A")

	writeCDN("B")
	// the cached answer and the opened database still hold the old content
	find("A")

	ResetCache()
	find("B")
}
