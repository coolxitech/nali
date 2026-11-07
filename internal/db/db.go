package db

import (
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/spf13/viper"

	"github.com/coolxitech/nali/pkg/common"
	"github.com/coolxitech/nali/pkg/dbif"
)

// GetDBs returns the databases selected for a query type. A selection may
// name several databases separated by commas, e.g. "czdb,ipinfo"; all of
// them are queried and their results shown side by side.
func GetDBs(typ dbif.QueryType) (dbs []dbif.DB) {
	dbs, err := resolveDBs(typ)
	if err != nil {
		// a bad database name has always been reported on its own, without
		// the prefix used for databases that could not be opened
		if errors.Is(err, errLookupFailed) {
			common.Fatalln(err)
		}
		common.Fatalln("Database init failed:", err)
	}
	return dbs
}

// errLookupFailed marks a database-name lookup failure; see lookupError.
var errLookupFailed = errors.New("database lookup failed")

// lookupError carries the message of a database-name lookup failure while
// matching errLookupFailed, so GetDBs keeps printing the message it always
// has. Its message is the one lookupDBByName reported.
type lookupError struct{ err error }

func (e *lookupError) Error() string { return e.err.Error() }
func (e *lookupError) Unwrap() error { return errLookupFailed }

// resolveDBs is the non-fatal form of GetDBs: it returns the databases selected
// for typ, or an error instead of terminating the process. Callers with their
// own error reporting (the TUI) use it directly, possibly from several
// goroutines at once; the caches it uses are guarded by cacheMu.
func resolveDBs(typ dbif.QueryType) ([]dbif.DB, error) {
	if dbs, found := cachedDBs(typ); found {
		return dbs, nil
	}

	lang := viper.GetString("selected.lang")
	if lang == "" {
		lang = "zh-CN"
	}

	var selectedKey string
	switch typ {
	case dbif.TypeIPv4:
		selectedKey = "selected.ipv4"
	case dbif.TypeIPv6:
		selectedKey = "selected.ipv6"
	case dbif.TypeDomain:
		selectedKey = "selected.cdn"
	default:
		return nil, fmt.Errorf("query type %d not supported", typ)
	}

	var dbs []dbif.DB
	for _, name := range strings.Split(viper.GetString(selectedKey), ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		info, err := resolveDBName(name)
		if err != nil {
			return nil, err
		}
		db, err := info.getOrError()
		if err != nil {
			return nil, err
		}
		dbs = append(dbs, db)
	}

	if len(dbs) == 0 {
		db, err := openDefaultDB(typ, lang)
		if err != nil {
			return nil, err
		}
		dbs = []dbif.DB{db}
	}

	storeDBs(typ, dbs)
	return dbs, nil
}

// resolveDBName resolves a configured database name, tagging a failure so that
// GetDBs can report it exactly as the CLI always has.
func resolveDBName(name string) (*DB, error) {
	info, err := lookupDBByName(name)
	if err != nil {
		return nil, &lookupError{err}
	}
	return info, nil
}

// defaultDBName is the built-in database used when no database is selected for
// typ: CDN for domain queries, the GeoIP2 database for a non-Chinese language,
// CZDB for IPv4 and the ZX IPv6 database for everything else.
func defaultDBName(typ dbif.QueryType, lang string) string {
	switch {
	case typ == dbif.TypeDomain:
		return "cdn"
	case lang != "zh-CN":
		return "geoip"
	case typ == dbif.TypeIPv4:
		return "czdb"
	default:
		return "zxipv6wry"
	}
}

// openDefaultDB opens the built-in database used when no database is selected
// for typ. It resolves the name like a configured database and opens it through
// the name cache, so the handle is shared with an explicit selection of the same
// database and ResetCache can close it; opening it outside the cache would leak
// the file handle across a database update (on Windows the replaced database
// could not even be renamed into place). The constructors return a non-nil error
// whenever the database is nil.
func openDefaultDB(typ dbif.QueryType, lang string) (dbif.DB, error) {
	info, err := resolveDBName(defaultDBName(typ, lang))
	if err != nil {
		return nil, err
	}
	return info.getOrError()
}

// Find queries every database selected for typ and returns one result per
// database in selection order; a failed lookup has a nil Result. It returns
// nil when every lookup failed.
func Find(typ dbif.QueryType, query string) []*Result {
	results, _ := findWith(typ, query, func(typ dbif.QueryType) ([]dbif.DB, error) {
		// GetDBs reports a resolution failure by terminating the process
		return GetDBs(typ), nil
	})
	return results
}

// FindOrError is Find for callers that must not have the process terminated
// when the selected databases cannot be resolved; such failures are returned
// as an error instead.
func FindOrError(typ dbif.QueryType, query string) ([]*Result, error) {
	return findWith(typ, query, resolveDBs)
}

// findWith implements Find and FindOrError; resolve supplies the databases of
// typ either fatally (GetDBs) or with an error (resolveDBs).
func findWith(typ dbif.QueryType, query string, resolve func(dbif.QueryType) ([]dbif.DB, error)) ([]*Result, error) {
	if results, found := queryCache.Load(query); found {
		return results.([]*Result), nil
	}
	// Convert NAT64 64:ff9b::/96 to IPv4
	if typ == dbif.TypeIPv6 {
		ip := net.ParseIP(query)
		if ip != nil {
			_, NAT64, _ := net.ParseCIDR("64:ff9b::/96")
			if NAT64.Contains(ip) {
				ip4 := make(net.IP, 4)
				copy(ip4, ip[12:16])
				query = ip4.String()
				typ = dbif.TypeIPv4
			}
		}
	}

	dbs, err := resolve(typ)
	if err != nil {
		return nil, err
	}

	var results []*Result
	found := false
	for _, db := range dbs {
		result, err := db.Find(query)
		if err != nil {
			result = nil
		} else {
			found = true
		}
		results = append(results, &Result{db.Name(), result})
	}
	if !found {
		return nil, nil
	}
	queryCache.Store(query, results)
	return results, nil
}
