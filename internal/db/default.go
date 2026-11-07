package db

import (
	"github.com/coolxitech/nali/pkg/cdn"
	"github.com/coolxitech/nali/pkg/czdb"
	"github.com/coolxitech/nali/pkg/ip2region"
)

func GetDefaultDBList() List {
	return List{
		&DB{
			Name: "czdb",
			NameAlias: []string{
				"chunzhen",
				"cz88",
			},
			Format:       FormatCZDB,
			File:         "ipv4.czdb",
			ZipEntry:     czdb.EntryIPv4,
			Languages:    LanguagesZH,
			Types:        TypesIPv4,
			DownloadUrls: []string{czdb.DownloadUrl},
		},
		&DB{
			Name: "czdb-ipv6",
			NameAlias: []string{
				"cz88-ipv6",
			},
			Format:       FormatCZDB,
			File:         "ipv6.czdb",
			ZipEntry:     czdb.EntryIPv6,
			Languages:    LanguagesZH,
			Types:        TypesIPv6,
			DownloadUrls: []string{czdb.DownloadUrl},
		},
		&DB{
			Name: "zxipv6wry",
			NameAlias: []string{
				"zxipv6",
				"zx",
			},
			Format:    FormatZXIPv6Wry,
			File:      "zxipv6wry.db",
			Languages: LanguagesZH,
			Types:     TypesIPv6,
		},
		&DB{
			Name: "geoip",
			NameAlias: []string{
				"geoip2",
				"geolite",
				"geolite2",
			},
			Format:    FormatMMDB,
			File:      "GeoLite2-City.mmdb",
			Languages: LanguagesAll,
			Types:     TypesIP,
		},
		&DB{
			Name:         "ipinfo",
			Format:       FormatMMDB,
			File:         "ipinfo_lite.mmdb",
			DownloadUrls: []string{"https://github.com/NetworkCats/IPinfoLite-Download/releases/latest/download/ipinfo_lite.mmdb"},
			Languages:    LanguagesEN,
			Types:        TypesIP,
		},
		&DB{
			Name: "dbip",
			NameAlias: []string{
				"db-ip",
			},
			Format:    FormatMMDB,
			File:      "dbip.mmdb",
			Languages: LanguagesAll,
			Types:     TypesIP,
		},
		&DB{
			Name:      "ipip",
			Format:    FormatIPIP,
			File:      "ipipfree.ipdb",
			Languages: LanguagesZH,
			Types:     TypesIP,
		},
		&DB{
			Name: "ip2region",
			NameAlias: []string{
				"i2r",
			},
			Format:       FormatIP2Region,
			File:         "ip2region.xdb",
			Languages:    LanguagesZH,
			Types:        TypesIPv4,
			DownloadUrls: ip2region.DownloadUrls,
		},
		&DB{
			Name: "ip2region-ipv6",
			NameAlias: []string{
				"i2r-ipv6",
			},
			Format:       FormatIP2Region,
			File:         "ip2region_v6.xdb",
			Languages:    LanguagesZH,
			Types:        TypesIPv6,
			DownloadUrls: ip2region.DownloadUrlsV6,
		},
		&DB{
			Name:      "ip2location",
			Format:    FormatIP2Location,
			File:      "IP2LOCATION-LITE-DB3.IPV6.BIN",
			Languages: LanguagesEN,
			Types:     TypesIP,
		},

		&DB{
			Name:         "cdn",
			Format:       FormatCDNYml,
			File:         "cdn.yml",
			Languages:    LanguagesZH,
			Types:        TypesCDN,
			DownloadUrls: cdn.DownloadUrls,
		},
	}
}
