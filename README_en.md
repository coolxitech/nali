<h1 align="center">
  <br>Nali<br>
</h1>

<h4 align="center">An offline tool for querying IP geographic information and CDN provider.</h4>

<p align="center">
  <a href="https://github.com/coolxitech/nali/actions">
    <img src="https://img.shields.io/github/actions/workflow/status/coolxitech/nali/go.yml?branch=master&style=flat-square" alt="Github Actions">
  </a>
  <a href="https://goreportcard.com/report/github.com/coolxitech/nali">
    <img src="https://goreportcard.com/badge/github.com/coolxitech/nali?style=flat-square">
  </a>
  <a href="https://github.com/coolxitech/nali/releases">
    <img src="https://img.shields.io/github/release/coolxitech/nali/all.svg?style=flat-square">
  </a>
  <a href="https://github.com/coolxitech/nali/releases">
    <img src="https://img.shields.io/github/downloads/coolxitech/nali/total?style=flat-square">
  </a>
</p>

#### [中文文档](https://github.com/coolxitech/nali/blob/master/README.md)

## Feature

- Multi database support
  - Chunzhen CZDB IPv4 / IPv6 database
  - ZX ipv6 database
  - ip2region IPv4 / IPv6 database
  - IPinfo Lite database
  - Geoip2 city database
  - IPIP free database
  - DB-IP database
  - IP2Location DB3 LITE database
- CDN provider query
- Pipeline support
- Interactive query
- Interactive terminal UI (TUI)
- Both ipv4 and ipv6 supported
- Multilingual support
- Offline query
- Full platform support
- Color print

## Install

### Install from source

Nali requires Go >= 1.26. You can build it from source:

```sh
$ go install github.com/zu1k/nali@latest
```

### Install pre-build binary

Pre-built binaries are available here: [release](https://github.com/zu1k/nali/releases)

Download the binary compatible with your platform, unpack and copy to the directory in path

### Debian / Ubuntu

Download the `.deb` package for your architecture (amd64, i386, armhf, arm64) from the [release](https://github.com/zu1k/nali/releases) page and install it:

```sh
$ sudo apt install ./nali_*_amd64.deb
```

## Usage

Before the first query with the Chunzhen CZDB database (the default IPv4 database) you need to fill in `key:` in the `czdb` entry of the config file (or set the `CZDB_KEY` environment variable), see [Use the Chunzhen CZDB database](#use-the-chunzhen-czdb-database).

### Query a simple IP address

```
$ nali 1.2.3.4
1.2.3.4 [澳大利亚 APNIC Debogon-prefix网络]
```

#### or use `pipe`

```
$ echo IP 6.6.6.6 | nali
IP 6.6.6.6 [美国 亚利桑那州华楚卡堡市美国国防部网络中心]
```

### Query multiple IP addresses

```
$ nali 1.2.3.4 4.3.2.1 123.23.3.0
1.2.3.4 [澳大利亚 APNIC Debogon-prefix网络]
4.3.2.1 [美国 新泽西州纽瓦克市Level3Communications]
123.23.3.0 [越南 越南邮电集团公司]
```

### Interactive query

use `exit` or  `quit` to quit

```
$ nali
123.23.23.23
123.23.23.23 [越南 越南邮电集团公司]
1.0.0.1
1.0.0.1 [美国 APNIC&CloudFlare公共DNS服务器]
8.8.8.8
8.8.8.8 [美国 加利福尼亚州圣克拉拉县山景市谷歌公司DNS服务器]
quit
```

### Interactive terminal UI (TUI)

`nali tui` opens a full-screen interactive interface for querying IP addresses, IPv6 addresses and domain names one after another. You can also paste a whole log line (for example the output of `nslookup`/`dig` or a line of an access log) and the interface picks out the IP addresses and domain names it contains. Every detected entry shows the results of all selected databases at once.

```
$ nali tui
```

The header shows the version on the left and the state of each database on the right: `✓` means the database file is in place and `✗` means the file is missing (a hint is then shown below the header, suggesting to run `nali update` first). The result area is in the middle, the input line and the key hints are at the bottom. Queries show progress without blocking the input line; results are grouped as "query line → each detected entry → each database's answer", and the last 200 queries are kept.

The screen looks roughly like this (results are placeholders; IP entries only match the IP databases, the CDN database only applies to domain names):

```
$ nali tui
nali v0.0.0             IPv4 czdb ✓  IPv6 zxipv6wry ✓  CDN cdn ✓

1. 1.2.3.4
   1.2.3.4  IPv4
     czdb       [IPv4 result]
2. 240e::1
   240e::1  IPv6
     zxipv6wry  [IPv6 result]
3. www.example.com
   www.example.com  Domain
     cdn        [domain CDN result]

> 输入 IP / IPv6 / 域名，或粘贴任意一行日志，enter 查询
enter 查询 · tab 切换 · ↑↓ 历史/滚动 · ? 帮助 · esc 关闭/退出
```

Queries are executed serially: one query runs while the others wait in the queue. A running block shows `查询中…`, a queued block shows `排队中…`, and a spinner with `查询中 N` (the number of queued plus running queries) is shown in front of the key hints at the bottom.

| Key | Action |
| --- | --- |
| `enter` | Submit the query (while the input line has focus) |
| `tab` / `shift+tab` | Switch focus between the input line and the result area |
| `↑` / `↓` | Browse query history when the input line has focus; scroll up and down when the result area has focus |
| `pgup` / `pgdn` (or `ctrl+u` / `ctrl+d`) | Scroll the result area by a page |
| `ctrl+l` | Clear the result area |
| `?` | Show/hide the help when the input line is empty or the result area has focus |
| `u` | Open the database update panel when the input line is empty or the result area has focus |
| `esc` | Close the help first, then clear the input, then quit |
| `ctrl+c` | Quit at any time |

The databases must be ready before using the TUI, run `nali update` first (see [Update database](#update-database)). When a database fails to initialize (for example the Chunzhen CZDB decryption key is missing) the interface shows the error but stays open, and the other databases can still be queried.

`nali tui` needs an interactive terminal. When stdin or stdout is redirected (pipes, CI, Docker and so on) it fails with an error and points you to `nali <IP>` or piping instead; use the pipeline described in [Use with any other program](#use-with-any-other-program) for batch queries.

#### Update databases (TUI)

There is no need to quit the interface and run `nali update`: press `u` in the TUI to update the databases and watch the progress of every database right there.

1. Press `u` to open the database update panel. It starts in a confirmation state and lists the databases that will be updated (czdb, zxipv6wry, ip2region and cdn by default; the larger czdb-ipv6, ip2region-ipv6 and ipinfo databases are updated as well when they are selected or already downloaded, following the same rules as `nali update`).
2. Press `enter` to start; every database gets a line of its own with its progress.
3. Press `esc` to cancel while the update is running, and `esc` again to close the panel once it has finished. `ctrl+c` quits the program at any time (and cancels the update).

```
数据库更新

czdb           [██████████░░░░░░]  63% 8.1 MB / 13.0 MB 3.2 MB/s 下载
zxipv6wry      [████████████████] 100% 6.4 MB / 6.4 MB  3.1 MB/s 校验
ip2region      [███████░░░░░░░░░]  45% 3.1 MB           1.8 MB/s 下载
cdn            [████████████████] 100% 2.4 MB / 2.4 MB           已是最新版本
ipinfo                                                           等待
ip2region-ipv6                                                   未更新

更新进行中 · esc 取消
```

From left to right every line shows the database name, a progress bar sized to the terminal width together with a percentage, the downloaded / total size (only the downloaded size when the server does not announce a total), the download speed, and the current stage. The stages are `下载` → `校验` → `写入` → `完成` / `失败` (download → verify → write → done / failed); a database that has not started yet shows `等待` without a progress bar, one that received no update by the end of the run shows `未更新`, and a cancelled one shows `已取消` (and is not counted as a success). A database that is already up to date is still downloaded in full and compared with the local content: when the content is identical the file is not rewritten and the line is marked `已是最新版本` (already up to date).

The hint line at the bottom follows the state: `enter 开始更新 · esc 关闭` while confirming; when a query is still running, confirming first shows `等待当前查询完成… · esc 关闭` and the update starts once that query has finished; `更新进行中 · esc 取消` while running, `正在取消…` after a cancellation, and `esc 关闭` once it has finished. A finished run also shows a summary line `成功 N 个 / 失败 M 个`, followed by ` · 已取消 C 个` for the databases cancelled in this run, by ` · 跳过 K 个` for those that never reported an event, and by "，失败原因见上面的错误" when something failed.

When a database fails, the reason is shown on its own line and the other databases keep updating; after the update the database states in the header (`✓` / `✗`) are refreshed according to the result. The update runs in the background, so the interface never freezes and `ctrl+c` still quits at any time; new queries cannot be submitted while it runs (the input line stays editable and the hint line says the update is running), and querying works again as soon as it finishes. Neither cancelling nor a failure damages the existing databases: the partially downloaded data is not written and the old database is left untouched. The arguments and meaning of the `nali update` command are unchanged, but in a terminal it now shows a progress panel as well (see [Update database](#update-database)).

Both share the same progress data: the `nali update` panel has exactly the columns and stages of the one shown here; the difference is that the TUI can also cancel the update and lets you query again as soon as it has finished.

### Use with `dig`

```
$ dig nali.zu1k.com +short | nali
104.28.2.115 [美国 CloudFlare公司CDN节点]
104.28.3.115 [美国 CloudFlare公司CDN节点]
172.67.135.48 [美国 CloudFlare节点]
```

### Use with `nslookup`

```
$ nslookup nali.zu1k.com 8.8.8.8 | nali
Server:         8.8.8.8 [美国 加利福尼亚州圣克拉拉县山景市谷歌公司DNS服务器]
Address:        8.8.8.8 [美国 加利福尼亚州圣克拉拉县山景市谷歌公司DNS服务器]#53

Non-authoritative answer:
Name:   nali.zu1k.com
Address: 104.28.3.115 [美国 CloudFlare公司CDN节点]
Name:   nali.zu1k.com
Address: 104.28.2.115 [美国 CloudFlare公司CDN节点]
Name:   nali.zu1k.com
Address: 172.67.135.48 [美国 CloudFlare节点]
```

### Use with any other program

Because nali can read the contents of the `stdin` pipeline, it can be used with any program.

```
bash abc.sh | nali
```

Nali will insert IP information after IP address.

### IPv6 support

Use like IPv4

```
$ nslookup google.com | nali
Server:         127.0.0.53 [局域网 IP]
Address:        127.0.0.53 [局域网 IP]#53

Non-authoritative answer:
Name:   google.com
Address: 216.58.211.110 [美国 Google全球边缘网络]
Name:   google.com
Address: 2a00:1450:400e:809::200e [荷兰Amsterdam Google Inc. 服务器网段]
```

### Query CDN provider

```
$ nslookup www.gov.cn | nali
Server:         127.0.0.53 [局域网 IP]
Address:        127.0.0.53 [局域网 IP]#53

Non-authoritative answer:
www.gov.cn      canonical name = www.gov.cn.bsgslb.cn [白山云 CDN].
www.gov.cn.bsgslb.cn [白山云 CDN]       canonical name = zgovweb.v.bsgslb.cn [白山云 CDN].
Name:   zgovweb.v.bsgslb.cn [白山云 CDN]
Address: 103.104.170.25 [新加坡 ]
Name:   zgovweb.v.bsgslb.cn [白山云 CDN]
Address: 2001:428:6402:21b::5 [美国Louisiana州Monroe Qwest Communications Company, LLC (CenturyLink)]
Name:   zgovweb.v.bsgslb.cn [白山云 CDN]
Address: 2001:428:6402:21b::6 [美国Louisiana州Monroe Qwest Communications Company, LLC (CenturyLink)]
```

## Interface

After nali runs for the first time, a configuration file `config.yaml` will be generated in the config directory (use `nali info` to extract info), the configuration file defines the database information.

A database is defined as follows:

```yaml
- name: geoip
  name-alias:
  - geolite
  - geolite2
  format: mmdb
  file: GeoLite2-City.mmdb
  languages:
  - ALL
  types:
  - IPv4
  - IPv6
```

Configuration files written by older versions keep working: databases missing from the config fall back to the built-in defaults.

### Databases

| Database | Name / aliases | Query type | Default file | Auto download |
| --- | --- | --- | --- | --- |
| Chunzhen CZDB IPv4 | `czdb`, `chunzhen`, `cz88` | IPv4 (default) | `ipv4.czdb` | ✓ (needs the dedicated link or CZDB_DOWNLOAD_KEY) |
| Chunzhen CZDB IPv6 | `czdb-ipv6`, `cz88-ipv6` | IPv6 | `ipv6.czdb` | ✓ (needs the dedicated link or CZDB_DOWNLOAD_KEY) |
| ZX IPv6 | `zxipv6wry`, `zxipv6`, `zx` | IPv6 (default) | `zxipv6wry.db` | ✓ |
| ip2region IPv4 | `ip2region`, `i2r` | IPv4 | `ip2region.xdb` | ✓ |
| ip2region IPv6 | `ip2region-ipv6`, `i2r-ipv6` | IPv6 | `ip2region_v6.xdb` | ✓ |
| IPinfo Lite | `ipinfo` | IPv4, IPv6 | `ipinfo_lite.mmdb` | ✓ |
| GeoIP2 / GeoLite2 City | `geoip`, `geoip2`, `geolite`, `geolite2` | IPv4, IPv6 | `GeoLite2-City.mmdb` | manual |
| DB-IP | `dbip`, `db-ip` | IPv4, IPv6 | `dbip.mmdb` | manual |
| IPIP | `ipip` | IPv4, IPv6 | `ipipfree.ipdb` | manual |
| IP2Location DB3 LITE | `ip2location` | IPv4, IPv6 | `IP2LOCATION-LITE-DB3.IPV6.BIN` | manual |
| CDN | `cdn` | domain (default) | `cdn.yml` | ✓ |

Databases with automatic download are fetched on first use when the file is missing. For the others, put the file in the data directory shown by `nali info`, or set an absolute `file` path in the config.

### Use the Chunzhen CZDB database

Chunzhen no longer maintains the old `qqwry.dat`, so nali uses the official CZDB format instead; `czdb` is the default IPv4 database (file `ipv4.czdb`, aliases `chunzhen` and `cz88`).

The keys come from the Chunzhen community edition authorization page <https://www.cz88.net/geo-public> (login and application required). The two keys it provides serve different purposes and are not interchangeable:

- `CZDB_DOWNLOAD_KEY`: download authorization key, only used to download database files from the official Chunzhen endpoint;
- `CZDB_KEY`: decryption key, used to decrypt the local `.czdb` file and required for queries.

Config files written by older versions are migrated on startup: entries with `format: qqwry` are dropped and `qqwry`/`chunzhen` in `selected.ipv4` become `czdb`, while the other entries and custom download URLs stay untouched; the built-in czdb definition (name `czdb`, aliases `chunzhen`/`cz88`, file `ipv4.czdb`, official download URL) is also written into the `databases` list — the dropped qqwry entry is replaced by czdb in place, and when the config has no czdb entry yet (for example after the migration of an earlier version) the czdb entry is prepended, so `nali info` lists it. An existing czdb entry is never duplicated or overwritten, so your custom settings are kept. The migration rewrites `config.yaml`, so comments in it may be lost; the old `qqwry.dat` is not removed automatically, delete it manually. `NALI_DB_IP4=qqwry` no longer works and fails with an error, use `czdb` instead.

The recommended way is to put the key directly into the database entry of the config file: edit `config.yaml` in the directory shown by `nali info` and turn the `czdb` entry into the following (replace both placeholders with the keys from the Chunzhen authorization page):

```yaml
- name: czdb
  name-alias: [chunzhen, cz88]
  format: czdb
  file: ipv4.czdb
  zip-entry: cz88_public_v4.czdb
  key: "<base64 key>"
  languages: [zh-CN]
  types: [IPv4]
  download-urls:
  - https://www.cz88.net/api/communityIpAuthorization/communityIpDbFile?fn=czdb&key=<download authorization key>
```

`key:` holds the decryption key, and `download-urls` may point directly at the dedicated download link Chunzhen provides (the link already contains the download authorization key), so neither queries nor automatic updates through `nali update` need any environment variable.

Note that the keys are stored in plain text in `config.yaml`, so do not commit that file to a repository or share it with others (it lives in the directory shown by `nali info`, not in the repository).

As an alternative (useful for CI or temporary use), the decryption key can be provided with the `CZDB_KEY` environment variable (the alias `NALI_CZDB_KEY` works as well); `key:` in the config file takes precedence over the environment variable:

```
export CZDB_KEY="<base64 key>"   # Linux / macOS

set CZDB_KEY=<base64 key>        # Windows cmd

$env:CZDB_KEY="<base64 key>"     # Windows PowerShell
```

Automatic download additionally needs a download authorization key. When the link in `download-urls` already carries it (as in the example above) nothing else is required; otherwise the link may keep the `{key}` placeholder and the key is taken from the `CZDB_DOWNLOAD_KEY` environment variable (the alias `NALI_CZDB_DOWNLOAD_KEY` works as well). Without either of them nothing is downloaded, so put `ipv4.czdb` in the data directory shown by `nali info`, or set an absolute `file` path in the config.

Minimal setup for a fresh install:

1. Fill in `key:` in the `czdb` entry of the config file first (or set the `CZDB_KEY` environment variable). Without it every IPv4 query exits with `Database init failed: 未配置 czdb 解密密钥`.
2. Put the dedicated download link provided by Chunzhen into `download-urls` if automatic downloads are wanted (or set the `CZDB_DOWNLOAD_KEY` environment variable); otherwise place `ipv4.czdb` manually as described above.

If you do not want to apply for Chunzhen authorization, use another database instead, for example by switching with environment variables:

```
$ NALI_DB_IP4=ip2region nali 8.8.8.8
$ NALI_DB_IP4=ipinfo nali 8.8.8.8
```

To use it permanently, set `selected.ipv4` to `ip2region` or `ipinfo` in the config file.

The optional IPv6 database `czdb-ipv6` (`ipv6.czdb`) works the same way and is selected with `NALI_DB_IP6=czdb-ipv6 nali 240e::1` or `selected.ipv6`; the default IPv6 database is still `zxipv6wry`.

### Use IPinfo Lite

[IPinfo Lite](https://ipinfo.io/lite) provides free country and ASN data for both IPv4 and IPv6. It is downloaded on first use:

```
$ NALI_DB_IP4=ipinfo NALI_DB_IP6=ipinfo nali 8.8.8.8 2001:4860:4860::8888
8.8.8.8 [United States AS15169 Google LLC]
2001:4860:4860::8888 [United States AS15169 Google LLC]

$ NALI_DB_IP4=ipinfo nali --json 8.8.8.8
{"type":0,"ip":"8.8.8.8","text":"United States AS15169 Google LLC","source":"ipinfo","info":{"network":"8.8.8.0/24","country":"United States","country_code":"US","continent":"North America","continent_code":"NA","asn":"AS15169","as_name":"Google LLC","as_domain":"google.com"}}
```

To use it permanently, set `selected.ipv4` and `selected.ipv6` to `ipinfo` in the config file.

Text output includes country, ASN and AS name. JSON includes `network`, `country`, `country_code`, `continent`, `continent_code`, `asn`, `as_name` and `as_domain`. When a record omits `network`, the matched CIDR is used. Names are the database's English values.

Databases with `format: mmdb` are detected as IPinfo from their metadata, so an `ipinfo_lite.mmdb` downloaded from your [IPinfo account](https://ipinfo.io/dashboard/downloads) works as well. The default download URL is the community mirror [NetworkCats/IPinfoLite-Download](https://github.com/NetworkCats/IPinfoLite-Download); change `download-urls` in the config to use another source:

```yaml
- name: ipinfo
  format: mmdb
  file: ipinfo_lite.mmdb
  download-urls:
  - https://github.com/NetworkCats/IPinfoLite-Download/releases/latest/download/ipinfo_lite.mmdb
  languages: [en]
  types: [IPv4, IPv6]
```

IPinfo Lite data is licensed under [CC BY-SA 4.0](https://creativecommons.org/licenses/by-sa/4.0/); please attribute IPinfo when you publish data derived from it.

### Use ip2region IPv6

ip2region ships separate IPv4 (`ip2region`) and IPv6 (`ip2region-ipv6`) database files, both downloaded on first use:

```
$ NALI_DB_IP4=ip2region NALI_DB_IP6=ip2region-ipv6 nali 223.5.5.5 240e::1
223.5.5.5 [中国|浙江省|杭州市|阿里|CN]
240e::1 [中国|北京市|北京市|电信|CN]
```

An existing `ip2region.xdb` from older versions keeps working, and the outdated download URL in old config files is migrated to `ip2region_v4.xdb` automatically.

### Help

```
$ nali --help
Usage:
  nali [flags]
  nali [command]

Available Commands:
  completion  Generate the autocompletion script for the specified shell
  help        Help about any command
  info        get the necessary information of nali
  tui         start an interactive terminal UI (TUI)
  update      update ip databases and cdn, update nali to latest version if -v

Flags:
      --color string   Colorize output: auto, always or never (NO_COLOR is honored in auto mode) (default "auto")
      --gbk            Decode input as GBK (detected automatically on Chinese Windows)
  -h, --help           help for nali
  -j, --json           Output in JSON format
  -v, --version        version for nali

Use "nali [command] --help" for more information about a command.
```

### Update database

Without arguments, `nali update` updates the Chunzhen CZDB, ZX IPv6, ip2region (IPv4) and CDN databases. The larger optional databases `czdb-ipv6`, `ip2region-ipv6` (~37 MB) and `ipinfo` (~24 MB) are only updated when they are selected (config `selected` or the `NALI_DB_*` environment variables) or already downloaded.

```
$ nali update
2026/09/30 09:51:48 正在下载最新 czdb 数据库...
2026/09/30 09:51:49 czdb 数据库下载成功: ipv4.czdb
2026/09/30 09:51:49 正在下载最新 cdn 数据库...
2026/09/30 09:51:50 cdn 数据库下载成功: cdn.yml
...
```

The czdb download URL contains a `{key}` placeholder that is replaced with the `CZDB_DOWNLOAD_KEY` environment variable; when the `download-urls` in the config already contain the dedicated download link provided by Chunzhen, that variable is not needed (see [Use the Chunzhen CZDB database](#use-the-chunzhen-czdb-database)).

The log lines above are what you get when the output is redirected (`nali update > log`, a pipe, CI); no control characters are emitted then. In a terminal `nali update` draws a panel that is redrawn in place instead: one line per database, with the same columns as the TUI update panel, the reason of a failing database indented below its own line, and a summary line at the end.

```
czdb      [██████████████░░░░░░]  70% 9.1 MB / 13.0 MB 3.2 MB/s 下载
zxipv6wry [████████████████████] 100% 6.4 MB / 6.4 MB  3.1 MB/s 校验
ip2region [░░░░░░░░░░░░░░░░░░░░]  --% 3.1 MB           1.8 MB/s 下载
cdn       [████████████████████] 100% 2.4 MB / 2.4 MB           已是最新版本
成功 4 个 / 失败 0 个
```

From left to right every line shows the database name, a 20 cell progress bar with a percentage, the downloaded / total size (only the downloaded size when the server does not announce a total, with `--%` as the percentage), the download speed, and the stage (`下载` → `校验` → `写入` → `完成` / `失败` / `已是最新版本`, that is download → verify → write → done / failed / already up to date). The summary line is `成功 N 个 / 失败 M 个`, followed by ` · 已取消 C 个` when databases were cancelled and ` · 跳过 K 个` when a database received no update event in this run. The panel is written to stderr only, so stdout stays clean; a narrow terminal drops the progress bar first, then the speed and finally the byte column instead of wrapping.

Update specific databases (comma separated, aliases allowed); these are always updated:

```
$ nali update --db czdb,cdn,ipinfo
```

When the download is identical to the local database it is not written again and nali reports it as up to date. Downloads are validated before they replace the local file, so a failed or invalid download keeps the existing database. Add `-v` to also update nali itself to the latest version.

### Specify database

Users can specify which database to use by setting the environment variables `NALI_DB_IP4`, `NALI_DB_IP6` and `NALI_DB_CDN`, or `selected` in the config file. Any name or alias from [Databases](#databases) can be used.

#### Query several databases at once

Separate database names with commas; each database's result is shown in its own brackets:

```
$ NALI_DB_IP4=czdb,ipinfo nali 8.8.8.8
8.8.8.8 [美国–加利福尼亚州–圣克拉拉–山景城 谷歌公司DNS服务器] [United States AS15169 Google LLC]
```

In JSON output `source`, `text` and `info` hold the result of the first database that has one. When several databases are selected, a `results` array lists every database's result in order; databases without a result have an empty `text` and a `null` `info`.

#### Windows

##### Use geoip db

```
set NALI_DB_IP4=geoip

or use powershell

$env:NALI_DB_IP4="geoip"
```

##### Use ipip db

```
set NALI_DB_IP6=ipip

or use powershell

$env:NALI_DB_IP6="ipip"
```

#### Linux

##### Use geoip db

```
export NALI_DB_IP4=geoip
```

##### Use ipip db

```
export NALI_DB_IP6=ipip
```

### Multilingual support

Set the environment variable `NALI_LANG` to choose the output language of the GeoIP2 / GeoLite2 / DB-IP databases; English is used when the database has no names in that language. The supported values are listed by the GeoIP2 database. Other databases provide a single language (IPinfo Lite is English).

```
# NALI_LANG=en NALI_DB_IP4=geoip nali 1.1.1.1
1.1.1.1 [Australia]
```

### Change directory

Set the environment variable `NALI_HOME` to specify the working directory where the configuration file and database are stored. You can also use absolute paths in the configuration file to specify other database paths.

Set the environment variable `NALI_CONFIG_HOME` to specify the configuration file directory and `NALI_DB_HOME` to specify the database file directory.

If no environment variable is specified, the XDG specification will be used, with the configuration file directory in `$XDG_CONFIG_HOME/nali` and the database file directory in `$XDG_DATA_HOME/nali`.

```
set NALI_HOME=D:\nalidb

or

export NALI_HOME=/home/nali
```

## FAQ

### No colors when nali is in the middle of a pipeline

Colors are only used when writing to a terminal by default. Use `--color always` to force them, e.g. `cmd | nali --color always | less -R`, and `--color never` or `NO_COLOR=1` to turn them off.

### Garbled Chinese text on Windows

On Simplified Chinese Windows (code page 936), input that is not valid UTF-8 is decoded as GBK automatically, so the output of `tracert`, `nslookup` and similar tools can be piped into nali directly. In a Windows console (terminal) nali switches the console code page to UTF-8 (65001) and restores it on exit, error exits included, so Chinese output is no longer garbled; environments that already use UTF-8, such as Windows Terminal, are left as they are. Automatic GBK/GB18030 decoding of input is unchanged. If text is still garbled, pass `--gbk` to always decode input as GBK.

### `tail -f log | jq . | nali` holds back the last lines

nali prints each line as soon as it is read. `jq` buffers its output when it is not writing to a terminal, so the last lines wait inside jq; use `jq --unbuffered`:

```sh
tail -f access.log | jq --unbuffered . | nali
```

### Filter CDN results

CDN matches for domain names have `source` set to `cdn` in JSON output, so they can be filtered with jq:

```sh
nali --json < domains.txt | jq -c 'select(.source == "cdn")'
```

## Development

Requires Go >= 1.26.

```sh
make test      # go test -race ./...
make lint      # go vet + golangci-lint
make all-arch  # cross-compile every release platform
```

Tests need neither network access nor real databases. The MMDB fixtures in `testdata/mmdb` are produced by the separate module `testdata/mmdb/generate`; regenerate them with:

```sh
cd testdata/mmdb/generate && go run .
```

## Thanks

- [纯真QQIP离线数据库](http://www.cz88.net)
- [纯真 CZDB 数据库解析](https://github.com/tagphi/czdb-search-golang)
- [ZX公网ipv6数据库](https://ip.zxinc.org/ipquery/)
- [Geoip2 city数据库](https://www.maxmind.com/en/geoip2-precision-city-service)
- [geoip2-golang解析器](https://github.com/oschwald/geoip2-golang)
- [maxminddb-golang](https://github.com/oschwald/maxminddb-golang)
- [IPinfo Lite](https://ipinfo.io/lite)
- [CDN provider数据库](https://github.com/SukkaLab/cdn)
- [IPIP数据库](https://www.ipip.net/product/ip.html)
- [IPIP数据库解析](https://github.com/ipipdotnet/ipdb-go)
- [ip2region数据库](https://github.com/lionsoul2014/ip2region)
- [IP2Location DB3 LITE](https://lite.ip2location.com/database/db3-ip-country-region-city) *use the IPv6 BIN as it contains both IPv4 & IPv6*
- [Cobra CLI库](https://github.com/spf13/cobra)

Thanks to JetBrains for the Open Source License 

<a href="https://www.jetbrains.com/?from=nali">
  <img src="assets/GoLand.svg">
</a>

## Author

**Nali** © [zu1k](https://github.com/zu1k), Released under the [MIT](./LICENSE) License.<br>
