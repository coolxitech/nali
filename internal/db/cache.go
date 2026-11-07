package db

import (
	"sync"

	"github.com/coolxitech/nali/pkg/dbif"
)

var (
	// cacheMu guards dbNameCache and dbTypeCache, the in-process caches of
	// opened databases. A database is opened while the lock is held, so two
	// goroutines never load the same database twice; the queries themselves
	// run outside the lock, so a slow lookup never blocks another goroutine.
	// No function called with the lock held may call back into these accessors.
	cacheMu sync.Mutex

	dbNameCache = make(map[string]dbif.DB)
	dbTypeCache = make(map[dbif.QueryType][]dbif.DB)
	queryCache  = sync.Map{}
)

// loadDB returns the database named name, opening it with open when it is not
// cached yet. open runs while cacheMu is held so that concurrent callers share
// one handle instead of loading the database twice; a failed open is not
// cached. open must not call loadDB, cachedDB, cachedDBs or storeDBs.
//
// Because open runs under the lock, callers must assume that the first open can
// be slow: a missing database file is downloaded inside open (geoip, ip2region,
// cdn and czdb all do that), which can take a while and blocks every other
// caller of the cache until it finishes. Do not call the lookup path from a UI
// main loop, and never while holding another lock.
func loadDB(name string, open func() (dbif.DB, error)) (dbif.DB, error) {
	cacheMu.Lock()
	defer cacheMu.Unlock()

	if db, found := dbNameCache[name]; found {
		return db, nil
	}

	db, err := open()
	if err != nil {
		return nil, err
	}

	dbNameCache[name] = db
	return db, nil
}

// cachedDB returns the already opened database named name, if any.
func cachedDB(name string) (dbif.DB, bool) {
	cacheMu.Lock()
	defer cacheMu.Unlock()

	db, found := dbNameCache[name]
	return db, found
}

// cachedDBs returns the databases already resolved for typ, if any.
func cachedDBs(typ dbif.QueryType) ([]dbif.DB, bool) {
	cacheMu.Lock()
	defer cacheMu.Unlock()

	dbs, found := dbTypeCache[typ]
	return dbs, found
}

// storeDBs caches the databases resolved for typ. Concurrent resolutions of the
// same type may both store; the entries are interchangeable because the opened
// databases themselves come from loadDB.
func storeDBs(typ dbif.QueryType, dbs []dbif.DB) {
	cacheMu.Lock()
	defer cacheMu.Unlock()

	dbTypeCache[typ] = dbs
}

// closer is implemented by the databases that hold a file handle or keep the
// whole database in memory; dbif.DB itself does not require Close.
type closer interface {
	Close() error
}

// ResetCache drops the opened databases and the cached lookups, so the next
// lookup reopens the database files from disk. It is what a database update
// calls once the files on disk have been replaced. Databases that can be closed
// are closed first.
func ResetCache() {
	cacheMu.Lock()
	for _, db := range dbNameCache {
		// Closing does not go through the cache, so it is safe under the lock;
		// a database that cannot be closed is simply dropped.
		if c, ok := db.(closer); ok && c != nil {
			_ = c.Close()
		}
	}
	// NameDBMap and TypeDBMap describe the configured databases, not the opened
	// handles, so they are left alone.
	clear(dbNameCache)
	clear(dbTypeCache)
	cacheMu.Unlock()

	// queryCache is a sync.Map that running queries read and write, so it is
	// emptied in place instead of being replaced.
	queryCache.Range(func(key, _ any) bool {
		queryCache.Delete(key)
		return true
	})
}

var (
	NameDBMap = make(NameMap)
	TypeDBMap = make(TypeMap)
)
