// Package czdb reads the new 纯真 (CZDB) IP database format through
// github.com/tagphi/czdb-search-golang and exposes it with the nali database
// interface (Find/Name).
//
// A CZDB file is laid out as
//
//	HyperHeaderBlock (4B version | 4B clientId | 4B encryptedBlockSize | encryptedBlock)
//	random padding (randomSize bytes, only known after decrypting encryptedBlock)
//	SuperBlock (17B: dbType | dbSize | startIndexPtr | headerBlockSize | endIndexPtr)
//	HeaderBlock (headerBlockSize bytes, 20B per entry: 16B start IP | 4B index pointer)
//	index blocks (startIndexPtr .. endIndexPtr)
//	columnSelection (4B) | geoSize (4B) | geo data (geoSize bytes, XOR encrypted)
//	data records referenced by the index blocks
//
// All integers are little endian and all pointers are relative to the file
// offset of the SuperBlock (i.e. the end of the random padding).
package czdb

import (
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/coolxitech/nali/pkg/common"
	db "github.com/tagphi/czdb-search-golang/pkg/db"
	"github.com/tagphi/czdb-search-golang/pkg/utils"
)

// Names of the two database files inside the official download archive.
const (
	EntryIPv4 = "cz88_public_v4.czdb"
	EntryIPv6 = "cz88_public_v6.czdb"
)

// DownloadUrl is the official community edition download endpoint. Both the
// IPv4 and the IPv6 database ship in the same zip archive. The {key}
// placeholder is replaced at run time with ResolveDownloadKey().
var DownloadUrl = "https://www.cz88.net/api/communityIpAuthorization/communityIpDbFile?fn=czdb&key={key}"

const (
	// dbName is the name reported through Name().
	dbName = "czdb"

	// downloadKeyPlaceholder is replaced by ResolveDownloadKey() in ExpandURLs.
	downloadKeyPlaceholder = "{key}"

	// notFoundSentinel is what the underlying library returns (with a nil
	// error) when an IP is not covered by the database.
	notFoundSentinel = "IP not found"

	// nullColumn is the placeholder the library substitutes for empty columns.
	nullColumn = "null"
)

var (
	errNotInitialized = errors.New("czdb 未初始化")
	errNotLoaded      = errors.New("czdb 数据库未能加载到内存")
	errNotFound       = errors.New("czdb 库中未找到该 IP")
	errClosed         = errors.New("czdb 数据库已关闭")
)

func init() {
	// The search library prints warnings to stdout by default, which would
	// corrupt nali's piped/json output; send them to stderr instead.
	utils.SetDebugOutput(os.Stderr)
}

// Options describes one CZDB database file.
type Options struct {
	// File is the local path of the database file.
	File string
	// Key is the base64 encoded AES key. When empty ResolveKey is consulted.
	Key string
	// ZipEntry selects the archive member to extract when the download
	// endpoint answers with a zip file. Empty means EntryIPv4.
	ZipEntry string
	// DownloadUrls overrides the download sources (it may also point at a bare
	// czdb mirror). Empty means []string{DownloadUrl}.
	DownloadUrls []string
}

// Result is the normalized, human readable answer of one lookup.
type Result string

// String implements fmt.Stringer.
func (r Result) String() string {
	return string(r)
}

// CZDB is a CZDB database opened in memory mode.
type CZDB struct {
	searcher *db.DBSearcher

	// The library loads the database binary lazily on the first Search call in
	// MEMORY mode, and that load is not safe for concurrent use. loadMu
	// serializes it and loaded publishes the result: Find only ever calls into
	// the library after DBBin is populated, so lookups themselves are
	// read-only and may run concurrently. loadErr remembers a failed load so a
	// broken database is reported once instead of being retried forever.
	loadMu  sync.Mutex
	loaded  atomic.Bool
	loadErr error

	closed    atomic.Bool
	closeOnce sync.Once
}

// New opens the CZDB database described by opts. When the file does not exist
// it is downloaded first (from the official zip archive or, for a custom
// DownloadUrls entry, as a bare czdb file).
func New(opts Options) (*CZDB, error) {
	if opts.File == "" {
		return nil, errors.New("未指定 czdb 文件路径")
	}

	key := ResolveKey(opts.Key)
	if key == "" {
		return nil, errors.New("未配置 czdb 解密密钥，请设置环境变量 CZDB_KEY（base64 编码的密钥），或在配置文件数据库条目里配置 key")
	}

	if _, err := os.Stat(opts.File); err != nil {
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("无法访问 czdb 文件 %s: %w", opts.File, err)
		}
		if err := downloadDatabase(opts); err != nil {
			return nil, err
		}
	}

	searcher, err := db.InitDBSearcher(opts.File, key, db.MEMORY)
	if err != nil {
		db.CloseDBSearcher(searcher)
		return nil, fmt.Errorf("无法解析 czdb 数据库 %s: %w", opts.File, err)
	}

	c := &CZDB{searcher: searcher}
	// Warm the database up so that later lookups never touch the file handle
	// (and never trigger the non-concurrent lazy load). A database that cannot
	// be loaded is useless: without this check every query would just report
	// "not found" and the corruption would stay invisible.
	if err := c.ensureLoaded(); err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("无法载入 czdb 数据库 %s: %w", opts.File, err)
	}
	return c, nil
}

// downloadDatabase fetches the database file and stores it after validating it,
// so an error page or a truncated download never replaces a usable file.
func downloadDatabase(opts Options) error {
	log.Println("文件不存在，尝试从网络获取最新 czdb 库")
	data, err := FetchEntry(opts.ZipEntry, opts.DownloadUrls)
	if err != nil {
		return err
	}
	if err := common.SaveFile(opts.File, data); err != nil {
		return fmt.Errorf("保存 czdb 库失败: %w", err)
	}
	return nil
}

// Find looks query up in the database. A miss is reported as an error, which
// is how nali distinguishes "this database has nothing to say" from a hit.
func (c *CZDB) Find(query string, params ...string) (result fmt.Stringer, err error) {
	if c == nil || c.searcher == nil {
		return nil, errNotInitialized
	}
	if c.closed.Load() {
		return nil, errClosed
	}
	if err := c.ensureLoaded(); err != nil {
		return nil, err
	}

	raw, err := db.Search(query, c.searcher)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(raw) == notFoundSentinel {
		return nil, errNotFound
	}
	return Result(formatResult(raw)), nil
}

// Name implements dbif.DB.
func (c *CZDB) Name() string {
	return dbName
}

// Close releases the database file handle. It is safe to call it more than
// once and from several goroutines.
func (c *CZDB) Close() error {
	if c == nil {
		return nil
	}
	c.closeOnce.Do(func() {
		c.closed.Store(true)
		db.CloseDBSearcher(c.searcher)
	})
	return nil
}

// ensureLoaded makes sure the database binary is in memory, so that every
// later lookup is a pure read. The first successful load is remembered; a
// failed load is remembered as well, so no lookup ever observes a partially
// written buffer and a broken database is reported consistently.
func (c *CZDB) ensureLoaded() error {
	if c.loaded.Load() {
		return nil
	}

	c.loadMu.Lock()
	defer c.loadMu.Unlock()

	if c.loaded.Load() {
		return nil
	}
	if c.loadErr != nil {
		return c.loadErr
	}
	if len(c.searcher.DBBin) == 0 {
		// The library fills DBBin on the first search; any error other than a
		// plain miss means the file content itself is unusable.
		if _, err := db.Search(warmupQuery(c.searcher.IPType), c.searcher); err != nil {
			c.loadErr = fmt.Errorf("%w: %v", errNotLoaded, err)
			return c.loadErr
		}
	}
	if len(c.searcher.DBBin) == 0 {
		c.loadErr = errNotLoaded
		return c.loadErr
	}
	// The library truncates DBBin to the number of bytes it actually read and
	// still reports success, so a short read would leave a half loaded database
	// behind that answers "not found" for everything past the cut. Compare the
	// loaded size with the size the SuperBlock promises instead.
	if super := c.searcher.SuperBlock; super != nil {
		want := int64(super.DbSize)
		got := int64(len(c.searcher.DBBin))
		if got != want {
			c.loadErr = fmt.Errorf("%w: 数据库不完整，期望 %d 字节，实际载入 %d 字节", errNotLoaded, want, got)
			return c.loadErr
		}
	}
	c.loaded.Store(true)
	return nil
}

// warmupQuery returns an address that is valid for the database's IP family;
// it only exists to trigger the library's lazy memory load.
func warmupQuery(ipType int32) string {
	if ipType == int32(utils.IPV6) {
		return "::"
	}
	return "0.0.0.0"
}

// formatResult normalizes the library output. The library returns the selected
// columns joined by "\t" followed by the record's "other data", and uses
// "null" for empty columns; nali wants a single space separated line.
func formatResult(raw string) string {
	fields := strings.Split(raw, "\t")
	kept := make([]string, 0, len(fields))
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field == "" || field == nullColumn {
			continue
		}
		kept = append(kept, field)
	}
	// strings.Fields collapses runs of spaces (and any remaining tab) and also
	// trims the result.
	return strings.Join(strings.Fields(strings.Join(kept, " ")), " ")
}

// ResolveKey returns configured when it is not empty, otherwise the CZDB_KEY
// environment variable, otherwise NALI_CZDB_KEY.
func ResolveKey(configured string) string {
	if configured != "" {
		return configured
	}
	if key := strings.TrimSpace(os.Getenv("CZDB_KEY")); key != "" {
		return key
	}
	return strings.TrimSpace(os.Getenv("NALI_CZDB_KEY"))
}

// ResolveDownloadKey returns the CZDB_DOWNLOAD_KEY environment variable,
// otherwise NALI_CZDB_DOWNLOAD_KEY.
func ResolveDownloadKey() string {
	if key := strings.TrimSpace(os.Getenv("CZDB_DOWNLOAD_KEY")); key != "" {
		return key
	}
	return strings.TrimSpace(os.Getenv("NALI_CZDB_DOWNLOAD_KEY"))
}

// ExpandURLs replaces the {key} placeholder of every URL with
// ResolveDownloadKey(). A URL that still needs a key when none is configured is
// an error, so the caller can bail out before doing any network request.
func ExpandURLs(urls []string) ([]string, error) {
	key := ResolveDownloadKey()
	expanded := make([]string, 0, len(urls))
	for _, url := range urls {
		if strings.Contains(url, downloadKeyPlaceholder) {
			if key == "" {
				return nil, errors.New("下载地址需要下载密钥，请设置环境变量 CZDB_DOWNLOAD_KEY")
			}
			url = strings.ReplaceAll(url, downloadKeyPlaceholder, key)
		}
		expanded = append(expanded, url)
	}
	return expanded, nil
}
