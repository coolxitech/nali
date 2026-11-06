package db

import (
	"strings"

	"github.com/zu1k/nali/pkg/cdn"
	"github.com/zu1k/nali/pkg/common"
	"github.com/zu1k/nali/pkg/czdb"
	"github.com/zu1k/nali/pkg/dbif"
	"github.com/zu1k/nali/pkg/geoip"
	"github.com/zu1k/nali/pkg/ip2location"
	"github.com/zu1k/nali/pkg/ip2region"
	"github.com/zu1k/nali/pkg/ipip"
	"github.com/zu1k/nali/pkg/zxipv6wry"
)

type DB struct {
	Name      string
	NameAlias []string `yaml:"name-alias,omitempty" mapstructure:"name-alias"`
	Format    Format
	File      string
	Key       string `yaml:"key,omitempty" mapstructure:"key"`             // CZDB 解密密钥（base64）
	ZipEntry  string `yaml:"zip-entry,omitempty" mapstructure:"zip-entry"` // 官方压缩包内条目名

	Languages []string `yaml:"languages,omitempty"`
	Types     []Type   `yaml:"types,omitempty"`

	DownloadUrls []string `yaml:"download-urls,omitempty" mapstructure:"download-urls"`
}

func (d *DB) get() (db dbif.DB) {
	if db, found := dbNameCache[d.Name]; found {
		return db
	}

	db, err := d.open()
	if err != nil {
		common.Fatalln("Database init failed:", err)
	}

	dbNameCache[d.Name] = db
	return
}

// open builds the database described by d without consulting or filling the
// name cache, so callers that need a database without caching it (or that want
// to report the error themselves) can use it directly.
func (d *DB) open() (db dbif.DB, err error) {
	filePath := d.File

	switch d.Format {
	case FormatCZDB:
		db, err = czdb.New(czdb.Options{File: filePath, Key: d.Key, ZipEntry: d.ZipEntry, DownloadUrls: d.DownloadUrls})
	case FormatZXIPv6Wry:
		db, err = zxipv6wry.NewZXwry(filePath)
	case FormatIPIP:
		db, err = ipip.NewIPIP(filePath)
	case FormatMMDB:
		db, err = geoip.NewGeoIP(filePath, d.DownloadUrls)
	case FormatIP2Region:
		db, err = ip2region.NewIp2Region(filePath, d.DownloadUrls)
	case FormatIP2Location:
		db, err = ip2location.NewIP2Location(filePath)
	case FormatCDNYml:
		db, err = cdn.NewCDN(filePath, d.DownloadUrls)
	default:
		panic("DB format not supported!")
	}

	return db, err
}

type Format string

const (
	FormatMMDB        Format = "mmdb"
	FormatCZDB        Format = "czdb"
	FormatZXIPv6Wry   Format = "zxipv6wry"
	FormatIPIP        Format = "ipip"
	FormatIP2Region   Format = "ip2region"
	FormatIP2Location Format = "ip2location"

	FormatCDNYml Format = "cdn-yml"
)

var (
	LanguagesAll = []string{"ALL"}
	LanguagesZH  = []string{"zh-CN"}
	LanguagesEN  = []string{"en"}
)

type Type string

const (
	TypeIPv4 Type = "IPv4"
	TypeIPv6 Type = "IPv6"
	TypeCDN  Type = "CDN"
)

var (
	TypesAll  = []Type{TypeIPv4, TypeIPv6, TypeCDN}
	TypesIP   = []Type{TypeIPv4, TypeIPv6}
	TypesIPv4 = []Type{TypeIPv4}
	TypesIPv6 = []Type{TypeIPv6}
	TypesCDN  = []Type{TypeCDN}
)

type List []*DB
type NameMap map[string]*DB
type TypeMap map[Type][]*DB

func (m *NameMap) From(dbs List) {
	for _, db := range dbs {
		(*m)[db.Name] = db

		if alias := db.NameAlias; alias != nil {
			for _, aName := range alias {
				(*m)[aName] = db
			}
		}
	}
}

func (m *TypeMap) From(dbs List) {
	for _, db := range dbs {
		for _, typ := range db.Types {
			dbsInType := (*m)[typ]
			if dbsInType == nil {
				dbsInType = []*DB{db}
			} else {
				dbsInType = append(dbsInType, db)
			}
			(*m)[typ] = dbsInType
		}
	}
}

// lookupDb finds a database by name or alias, first in the user config and
// then in the built-in defaults.
func lookupDb(name string) (*DB, bool) {
	if dbInfo, found := NameDBMap[name]; found {
		return dbInfo, true
	}

	defaultNameDBMap := NameMap{}
	defaultNameDBMap.From(GetDefaultDBList())
	if dbInfo, found := defaultNameDBMap[name]; found {
		return dbInfo, true
	}
	return nil, false
}

func getDbByName(name string) (db *DB) {
	// The retired qqwry database has no reader any more, so a name pointing at
	// it must not fall through to the generic "not found" message.
	if strings.EqualFold(strings.TrimSpace(name), "qqwry") {
		common.Fatalf("旧纯真 qqwry 库已被 czdb 取代：请设置环境变量 CZDB_KEY，并使用 NALI_DB_IP4=czdb（或 chunzhen/cz88）\n")
		return
	}

	if dbInfo, found := lookupDb(name); found {
		return dbInfo
	}

	common.Fatalf("DB with name %s not found!\n", name)
	return
}

type Result struct {
	Source string
	common.Result
}

// Found reports whether the database answered the query.
func (r *Result) Found() bool {
	return r != nil && r.Result != nil
}

// Text returns the result's text, or "" when the database had no answer.
func (r *Result) Text() string {
	if !r.Found() {
		return ""
	}
	return r.String()
}
