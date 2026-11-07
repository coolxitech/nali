<h1 align="center">
  <br>Nali<br>
</h1>

<h4 align="center">一个查询IP地理信息和CDN提供商的离线终端工具.</h4>

<p align="center">
  <a href="https://github.com/zu1k/nali/actions">
    <img src="https://img.shields.io/github/actions/workflow/status/zu1k/nali/go.yml?branch=master&style=flat-square" alt="Github Actions">
  </a>
  <a href="https://goreportcard.com/report/github.com/zu1k/nali">
    <img src="https://goreportcard.com/badge/github.com/zu1k/nali?style=flat-square">
  </a>
  <a href="https://github.com/zu1k/nali/releases">
    <img src="https://img.shields.io/github/release/zu1k/nali/all.svg?style=flat-square">
  </a>
  <a href="https://github.com/zu1k/nali/releases">
    <img src="https://img.shields.io/github/downloads/zu1k/nali/total?style=flat-square">
  </a>
</p>

#### [English](https://github.com/zu1k/nali/blob/master/README_en.md)

## 功能

- 支持多种数据库
  - 纯真 CZDB IPv4 / IPv6 离线数据库
  - ZX IPv6 离线数据库
  - ip2region IPv4 / IPv6 数据库 (可选)
  - IPinfo Lite 数据库 (可选)
  - Geoip2 城市数据库 (可选)
  - IPIP 数据库 (可选)
  - DB-IP 数据库 (可选)
  - IP2Location DB3 LITE 数据库 (可选)
- CDN 服务提供商查询
- 支持管道处理
- 支持交互式查询
- 支持交互式终端界面（TUI）
- 同时支持IPv4和IPv6
- 支持多语言
- 查询完全离线
- 全平台支持
- 支持彩色输出

## 安装

### 从源码安装

Nali 需要预先安装 Go >= 1.26. 安装后可以从源码安装软件:

```sh
$ go install github.com/zu1k/nali@latest
```

### 下载预编译的可执行程序

可以从Release页面下载预编译好的可执行程序: [Release](https://github.com/zu1k/nali/releases)

你需要选择适合你系统和硬件架构的版本下载，解压后可直接运行

### Arch 系 Linux

我们在 Aur 中发布了 3 个相关的包:

- `nali-go`: Release 版本，安装时编译
- `nali-go-bin`: Release 版本，预编译的二进制文件
- `nali-go-git`: 最新的 master 分支版本，安装时编译

### Debian / Ubuntu

从 [Release](https://github.com/zu1k/nali/releases) 页面下载对应架构（amd64、i386、armhf、arm64）的 `.deb` 包后安装：

```sh
$ sudo apt install ./nali_*_amd64.deb
```

## 使用说明

首次使用纯真 CZDB 库（默认 IPv4 数据库）前，需要在配置文件的 `czdb` 条目里填好 `key:`（或设置环境变量 `CZDB_KEY`），详见[使用纯真 CZDB 库](#使用纯真-czdb-库)。

### 查询一个IP的地理信息

```
$ nali 1.2.3.4
1.2.3.4 [澳大利亚 APNIC Debogon-prefix网络]
```

#### 或者 使用 `管道`

```
$ echo IP 6.6.6.6 | nali
IP 6.6.6.6 [美国 亚利桑那州华楚卡堡市美国国防部网络中心]
```

### 同时查询多个IP的地理信息

```
$ nali 1.2.3.4 4.3.2.1 123.23.3.0
1.2.3.4 [澳大利亚 APNIC Debogon-prefix网络]
4.3.2.1 [美国 新泽西州纽瓦克市Level3Communications]
123.23.3.0 [越南 越南邮电集团公司]
```

### 交互式查询

使用 `exit` 或  `quit` 退出查询

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

### 交互式终端界面（TUI）

`nali tui` 提供一个全屏的交互式界面，用来连续查询 IP、IPv6 和域名，也可以直接粘贴整行日志（例如 `nslookup`、`dig` 的输出或一行访问日志），界面会自动识别其中的 IP 与域名。每个识别到的条目都会一次性展示所有已选数据库的结果。

```
$ nali tui
```

界面顶部左边是版本号，右边是各数据库的状态：`✓` 表示该数据库的文件已就位，`✗` 表示文件缺失（此时标题下方会多出一行提示，建议先执行 `nali update`）。中间是结果区，底部是输入行与按键提示。查询期间有进度提示，输入框不会卡住；结果按「查询行 → 每个识别到的条目 → 每个数据库的答案」分组显示，并保留最近 200 条查询。

下面是界面示意（其中的结果为占位说明；IP 条目只命中 IP 库，CDN 库只对域名生效）：

```
$ nali tui
nali v0.0.0             IPv4 czdb ✓  IPv6 zxipv6wry ✓  CDN cdn ✓

1. 1.2.3.4
   1.2.3.4  IPv4
     czdb       [IPv4 查询结果]
2. 240e::1
   240e::1  IPv6
     zxipv6wry  [IPv6 查询结果]
3. www.example.com
   www.example.com  Domain
     cdn        [域名 CDN 查询结果]

> 输入 IP / IPv6 / 域名，或粘贴任意一行日志，enter 查询
enter 查询 · tab 切换 · ↑↓ 历史/滚动 · ? 帮助 · esc 关闭/退出
```

查询是串行执行的：一次只查一条，其余排队等待。执行中的块显示 `查询中…`，排队中的块显示 `排队中…`，底部提示行前会显示 spinner 与 `查询中 N`（N 为排队与执行中的总数）。

| 按键 | 作用 |
| --- | --- |
| `enter` | 提交查询（输入框获得焦点时） |
| `tab` / `shift+tab` | 在输入框与结果区之间切换焦点 |
| `↑` / `↓` | 输入框聚焦时翻历史查询；结果区聚焦时上下滚动 |
| `pgup` / `pgdn`（或 `ctrl+u` / `ctrl+d`） | 结果区整页滚动 |
| `ctrl+l` | 清空结果区 |
| `?` | 输入框为空或结果区聚焦时显示/隐藏帮助 |
| `u` | 输入框为空或结果区聚焦时打开数据库更新面板 |
| `esc` | 先关闭帮助，其次清空输入，最后退出程序 |
| `ctrl+c` | 随时退出 |

TUI 使用的数据库需要事先就绪，请先执行 `nali update`（见[更新数据库](#更新数据库)）。如果某个数据库初始化失败（例如没有配置纯真 CZDB 的解密密钥），界面会显示错误提示但不会退出，其余数据库仍可继续查询。

`nali tui` 需要在交互式终端中运行。stdin 或 stdout 被重定向时（管道、CI、Docker 等）会直接报错并提示改用 `nali <IP>` 或管道方式，此时请用[与任意程序配合使用](#与任意程序配合使用)中的管道方式完成批量查询。

#### 更新数据库（TUI）

不必退出界面再执行 `nali update`：在 TUI 里按 `u` 就能更新数据库，并直接看到每个数据库的下载进度。

1. 按 `u` 打开「数据库更新」面板，面板先是确认态，列出将要更新的数据库（默认 czdb、zxipv6wry、ip2region、cdn；已选用或已下载的 czdb-ipv6、ip2region-ipv6、ipinfo 也会一并更新，规则与 `nali update` 相同）。
2. 按 `enter` 开始更新，每个数据库占一行显示进度。
3. 更新过程中按 `esc` 取消；更新结束后按 `esc` 关闭面板。`ctrl+c` 随时退出程序（同时取消更新）。

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

每一行从左到右依次是：数据库名、随终端宽度自适应的进度条与百分比、已下载 / 总大小（服务器未公布大小时只显示已下载）、下载速度，以及当前阶段。阶段依次是 `下载` → `校验` → `写入` → `完成` / `失败`；还没开始更新的数据库显示 `等待`（没有进度条），整轮结束后仍未更新的数据库显示 `未更新`，被取消的数据库显示 `已取消`（不计入成功数）。已是最新版本的数据库仍会完整下载并与本地内容比对，内容相同时不重写文件，该行直接标记为 `已是最新版本`。

底部提示行随状态变化：确认态是 `enter 开始更新 · esc 关闭`；若有查询正在执行，确认后会先显示 `等待当前查询完成… · esc 关闭`，等该查询结束后才开始更新；更新中是 `更新进行中 · esc 取消`，取消后是 `正在取消…`，结束后是 `esc 关闭`。结束后另有一行汇总：`成功 N 个 / 失败 M 个`，本轮被取消的库追加 ` · 已取消 C 个`，整轮结束后仍未收到事件的库追加 ` · 跳过 K 个`，有失败时末尾再追加「，失败原因见上面的错误」。

某个数据库失败时，失败原因显示在它自己那一行，其余数据库继续更新；更新结束后标题栏的数据库状态（`✓` / `✗`）会按更新结果刷新。更新在后台进行，界面不会卡住，`ctrl+c` 仍然可以随时退出；更新期间不能提交新查询（输入框仍可编辑，提示行会显示「更新进行中」），更新结束后即可继续查询。取消或失败都不会破坏已有的数据库：已下载的部分不写入，旧数据库保持原样。命令行方式 `nali update` 的参数与语义不变，但它在终端下也会显示进度面板（见[更新数据库](#更新数据库)）。

两者共用同一套进度数据：`nali update` 的进度面板与这里的列和阶段完全一致，区别是在 TUI 里可以取消更新，更新结束后还能直接继续查询。

### 与 `dig` 命令配合使用

需要你系统中已经安装好 dig 程序

```
$ dig nali.zu1k.com +short | nali
104.28.2.115 [美国 CloudFlare公司CDN节点]
104.28.3.115 [美国 CloudFlare公司CDN节点]
172.67.135.48 [美国 CloudFlare节点]
```

### 与 `nslookup` 命令配合使用

需要你系统中已经安装好 nslookup 程序

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

### 与任意程序配合使用

因为 nali 支持管道处理，所以可以和任意程序配合使用

```
bash abc.sh | nali
```

Nali 将在 IP后面插入IP地理信息，CDN域名后面插入CDN服务提供商信息

### 支持IPv6

和 IPv4 用法完全相同

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

### 查询 CDN 服务提供商

因为 CDN 服务通常使用 CNAME 的域名解析方式，所以推荐与 `nslookup` 或者 `dig` 配合使用，在已经知道 CNAME 后可单独使用

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

## 用户交互

程序第一次运行后，会在 config 目录生成配置文件 `config.yaml` (使用 `nali info` 来查看具体信息)，配置文件定义了数据库信息，默认用户无需进行修改

数据库格式默认如下：

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

其中，`languages` 和 `types` 表示该数据库支持的语言和查询类型。 如果你需要增加数据库，需小心修改配置文件，如果有任何问题，欢迎提 issue 询问。

旧版本生成的配置文件无需修改：配置文件中找不到的数据库会使用内置的默认定义。

### 数据库一览

| 数据库 | 名称 / 别名 | 查询类型 | 默认文件 | 自动下载 |
| --- | --- | --- | --- | --- |
| 纯真 CZDB IPv4 | `czdb`, `chunzhen`, `cz88` | IPv4（默认） | `ipv4.czdb` | ✓（需专属下载链接或 CZDB_DOWNLOAD_KEY） |
| 纯真 CZDB IPv6 | `czdb-ipv6`, `cz88-ipv6` | IPv6 | `ipv6.czdb` | ✓（需专属下载链接或 CZDB_DOWNLOAD_KEY） |
| ZX IPv6 | `zxipv6wry`, `zxipv6`, `zx` | IPv6（默认） | `zxipv6wry.db` | ✓ |
| ip2region IPv4 | `ip2region`, `i2r` | IPv4 | `ip2region.xdb` | ✓ |
| ip2region IPv6 | `ip2region-ipv6`, `i2r-ipv6` | IPv6 | `ip2region_v6.xdb` | ✓ |
| IPinfo Lite | `ipinfo` | IPv4、IPv6 | `ipinfo_lite.mmdb` | ✓ |
| GeoIP2 / GeoLite2 City | `geoip`, `geoip2`, `geolite`, `geolite2` | IPv4、IPv6 | `GeoLite2-City.mmdb` | 需手动下载 |
| DB-IP | `dbip`, `db-ip` | IPv4、IPv6 | `dbip.mmdb` | 需手动下载 |
| IPIP | `ipip` | IPv4、IPv6 | `ipipfree.ipdb` | 需手动下载 |
| IP2Location DB3 LITE | `ip2location` | IPv4、IPv6 | `IP2LOCATION-LITE-DB3.IPV6.BIN` | 需手动下载 |
| CDN | `cdn` | 域名（默认） | `cdn.yml` | ✓ |

支持自动下载的数据库在首次使用、本地文件不存在时会自动下载；需手动下载的数据库请将文件放入 `nali info` 显示的数据目录，或在配置文件中用绝对路径指定 `file`。

### 使用纯真 CZDB 库

纯真已经停止维护旧的 `qqwry.dat`，nali 改用纯真官方新版 CZDB 格式，默认的 IPv4 数据库是 `czdb`（文件 `ipv4.czdb`，别名 `chunzhen`、`cz88`）。

密钥来自纯真社区版授权页 <https://www.cz88.net/geo-public>（需登录并申请）。该页面提供的两个密钥用途不同，不能混用：

- `CZDB_DOWNLOAD_KEY`：下载授权密钥，只用于从纯真官方接口下载数据库文件；
- `CZDB_KEY`：解密密钥，用于解密本地的 `.czdb` 文件，查询时必需。

旧版本生成的配置文件会在启动时自动迁移：`format` 为 `qqwry` 的数据库条目被移除，`selected.ipv4` 中的 `qqwry`、`chunzhen` 改为 `czdb`，其它数据库条目与自定义下载地址保持不变；同时把 czdb 的默认定义（名称 `czdb`、别名 `chunzhen`/`cz88`、文件 `ipv4.czdb`、官方下载地址）写进 `databases` 列表——被移除的 qqwry 条目就地替换为 czdb，若配置里还没有 czdb 条目（例如已由更早的版本迁移过），则把 czdb 条目插到列表最前面，因此 `nali info` 能看到它；已有 czdb 条目时不会重复添加，也不会覆盖你的自定义设置。迁移会重写 `config.yaml`，原有注释可能丢失；旧的 `qqwry.dat` 不会被自动删除，可手动清理。`NALI_DB_IP4=qqwry` 不再可用，会直接报错，请改成 `czdb`。

推荐把密钥直接写进配置文件的数据库条目：编辑 `nali info` 显示的目录下的 `config.yaml`，把 `czdb` 条目改成下面这样（把两个占位符换成纯真授权页给出的密钥）：

```yaml
- name: czdb
  name-alias: [chunzhen, cz88]
  format: czdb
  file: ipv4.czdb
  zip-entry: cz88_public_v4.czdb
  key: "<base64密钥>"
  languages: [zh-CN]
  types: [IPv4]
  download-urls:
  - https://www.cz88.net/api/communityIpAuthorization/communityIpDbFile?fn=czdb&key=<下载授权密钥>
```

其中 `key:` 是解密密钥；`download-urls` 可以直接写纯真提供的专属下载链接（链接里已经含下载授权密钥），这样查询和 `nali update` 自动更新都不需要任何环境变量。

注意：密钥会以明文保存在 `config.yaml` 里，请不要把该配置文件提交到仓库或分享给他人（该文件在 `nali info` 显示的目录中，不在仓库内）。

作为替代方式（适合 CI 或临时使用），也可以用环境变量 `CZDB_KEY` 提供解密密钥（兼容 `NALI_CZDB_KEY`），配置文件里的 `key:` 优先于环境变量：

```
export CZDB_KEY="<base64密钥>"   # Linux / macOS

set CZDB_KEY=<base64密钥>        # Windows cmd

$env:CZDB_KEY="<base64密钥>"     # Windows PowerShell
```

自动下载 CZDB 还需要下载授权密钥。如果 `download-urls` 里的链接已经带上授权密钥（如上例），无需再配置；否则可以让链接保留 `{key}` 占位符，并通过环境变量 `CZDB_DOWNLOAD_KEY` 提供该密钥（兼容 `NALI_CZDB_DOWNLOAD_KEY`）。两种方式都没有时不会自动下载，请手动把 `ipv4.czdb` 放到 `nali info` 显示的数据目录，或在配置文件中用绝对路径指定 `file`。

全新安装的最小步骤：

1. 先在配置文件的 `czdb` 条目里填好 `key:`（或设置环境变量 `CZDB_KEY`）。缺少密钥时任何 IPv4 查询都会直接以 `Database init failed: 未配置 czdb 解密密钥` 退出。
2. 需要自动下载时，再把纯真提供的专属下载链接填进 `download-urls`（或设置环境变量 `CZDB_DOWNLOAD_KEY`）；否则按上面的说明手动放置 `ipv4.czdb`。

不想申请纯真授权时，也可以改用其它数据库，例如临时用环境变量切换：

```
$ NALI_DB_IP4=ip2region nali 8.8.8.8
$ NALI_DB_IP4=ipinfo nali 8.8.8.8
```

长期使用可在配置文件中把 `selected.ipv4` 改成 `ip2region` 或 `ipinfo`。

IPv6 可选数据库 `czdb-ipv6`（`ipv6.czdb`）用法相同，通过 `NALI_DB_IP6=czdb-ipv6 nali 240e::1` 或配置中的 `selected.ipv6` 选用；默认的 IPv6 数据库仍然是 `zxipv6wry`。

### 使用 IPinfo Lite

[IPinfo Lite](https://ipinfo.io/lite) 提供免费的国家和 ASN 数据，同时支持 IPv4 和 IPv6，首次使用时自动下载：

```
$ NALI_DB_IP4=ipinfo NALI_DB_IP6=ipinfo nali 8.8.8.8 2001:4860:4860::8888
8.8.8.8 [United States AS15169 Google LLC]
2001:4860:4860::8888 [United States AS15169 Google LLC]

$ NALI_DB_IP4=ipinfo nali --json 8.8.8.8
{"type":0,"ip":"8.8.8.8","text":"United States AS15169 Google LLC","source":"ipinfo","info":{"network":"8.8.8.0/24","country":"United States","country_code":"US","continent":"North America","continent_code":"NA","asn":"AS15169","as_name":"Google LLC","as_domain":"google.com"}}
```

长期使用可在配置文件中将 `selected.ipv4`、`selected.ipv6` 设为 `ipinfo`。

文本输出包含国家、ASN 和 AS 名称；JSON 输出包含 `network`、`country`、`country_code`、`continent`、`continent_code`、`asn`、`as_name`、`as_domain`。数据库记录未提供 `network` 字段时，使用命中的 CIDR 网段。名称为数据库中的英文原文。

`format` 为 `mmdb` 的数据库会根据元数据自动识别是否为 IPinfo 格式，因此也可以使用登录 [IPinfo](https://ipinfo.io/dashboard/downloads) 后下载的 `ipinfo_lite.mmdb`。默认下载地址是社区维护的镜像 [NetworkCats/IPinfoLite-Download](https://github.com/NetworkCats/IPinfoLite-Download)，可以在配置文件中通过 `download-urls` 改为其他地址：

```yaml
- name: ipinfo
  format: mmdb
  file: ipinfo_lite.mmdb
  download-urls:
  - https://github.com/NetworkCats/IPinfoLite-Download/releases/latest/download/ipinfo_lite.mmdb
  languages: [en]
  types: [IPv4, IPv6]
```

IPinfo Lite 数据以 [CC BY-SA 4.0](https://creativecommons.org/licenses/by-sa/4.0/) 许可发布，公开使用其数据时请注明来源 IPinfo。

### 使用 ip2region IPv6

ip2region 分为 IPv4（`ip2region`）和 IPv6（`ip2region-ipv6`）两个数据库文件，首次使用时自动下载：

```
$ NALI_DB_IP4=ip2region NALI_DB_IP6=ip2region-ipv6 nali 223.5.5.5 240e::1
223.5.5.5 [中国|浙江省|杭州市|阿里|CN]
240e::1 [中国|北京市|北京市|电信|CN]
```

旧版本的 `ip2region.xdb` 文件仍可直接使用；配置文件中失效的旧下载地址会在启动时自动迁移到 `ip2region_v4.xdb`。

### 查看帮助

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

### 更新数据库

不带参数时更新纯真 CZDB、ZX IPv6、ip2region (IPv4) 和 CDN 数据库。体积较大的可选数据库 `czdb-ipv6`、`ip2region-ipv6`（约 37 MB）和 `ipinfo`（约 24 MB）只在被选用（配置文件的 `selected` 或 `NALI_DB_*` 环境变量）或本地已存在时才会更新。

```
$ nali update
2026/09/30 09:51:48 正在下载最新 czdb 数据库...
2026/09/30 09:51:49 czdb 数据库下载成功: ipv4.czdb
2026/09/30 09:51:49 正在下载最新 cdn 数据库...
2026/09/30 09:51:50 cdn 数据库下载成功: cdn.yml
...
```

czdb 的下载地址含 `{key}` 占位符，会替换为环境变量 `CZDB_DOWNLOAD_KEY` 的值；如果配置文件的 `download-urls` 里直接写的是纯真提供的专属下载链接，则无需设置该环境变量（见[使用纯真 CZDB 库](#使用纯真-czdb-库)）。

上面的日志是输出被重定向（`nali update > log`、管道、CI）时的样子，不会出现控制字符。在终端里运行时则是原地重绘的进度面板：每个数据库一行，列序与 TUI 的更新面板一致；失败的库会把原因缩进显示在自己那一行下方，结束时打印汇总行。

```
czdb      [██████████████░░░░░░]  70% 9.1 MB / 13.0 MB 3.2 MB/s 下载
zxipv6wry [████████████████████] 100% 6.4 MB / 6.4 MB  3.1 MB/s 校验
ip2region [░░░░░░░░░░░░░░░░░░░░]  --% 3.1 MB           1.8 MB/s 下载
cdn       [████████████████████] 100% 2.4 MB / 2.4 MB           已是最新版本
成功 4 个 / 失败 0 个
```

每行从左到右依次是：库名、20 格进度条与百分比、已下载 / 总大小（服务器未公布大小时只显示已下载，百分比显示 `--%`）、下载速度、阶段（`下载` → `校验` → `写入` → `完成` / `失败` / `已是最新版本`）。汇总行是 `成功 N 个 / 失败 M 个`，有库被取消时追加 ` · 已取消 C 个`，有库本轮没有收到更新事件时追加 ` · 跳过 K 个`。进度面板只写到 stderr，stdout 保持干净；终端太窄时会依次丢掉进度条、速度和字节列，不会折行溢出。

或者指定数据库（逗号分隔，支持别名），指定的数据库总会更新：

```
$ nali update --db czdb,cdn,ipinfo
```

下载的内容与本地数据库相同时不会重新写入，并显示 `数据库已是最新版本`。下载的数据会先校验再替换本地文件，下载失败或内容无效时保留原有的数据库。加上 `-v` 参数会同时将 nali 更新到最新版本。

### 自选数据库

用户可以指定使用哪个数据库，需要设置环境变量 `NALI_DB_IP4`、`NALI_DB_IP6`、`NALI_DB_CDN`，或者修改配置文件中的 `selected`。可以使用[数据库一览](#数据库一览)中的任意名称或别名。

#### 同时查询多个数据库

用逗号分隔多个数据库名称，每个数据库的结果显示在各自的方括号中：

```
$ NALI_DB_IP4=czdb,ipinfo nali 8.8.8.8
8.8.8.8 [美国–加利福尼亚州–圣克拉拉–山景城 谷歌公司DNS服务器] [United States AS15169 Google LLC]
```

JSON 输出中 `source`、`text`、`info` 为第一个有结果的数据库的结果；选择了多个数据库时，另有 `results` 数组按顺序列出每个数据库的结果，没有结果的数据库 `text` 为空、`info` 为 `null`。

#### Windows平台

##### 使用geoip数据库

```
set NALI_DB_IP4=geoip

或者使用 powershell

$env:NALI_DB_IP4="geoip"
```

##### 使用ipip数据库

```
set NALI_DB_IP6=ipip

或者使用 powershell

$env:NALI_DB_IP6="ipip"
```

#### Linux平台

##### 使用geoip数据库

```
export NALI_DB_IP4=geoip
```

##### 使用ipip数据库

```
export NALI_DB_IP4=ipip
```

### 多语言支持

通过环境变量 `NALI_LANG` 指定 GeoIP2 / GeoLite2 / DB-IP 数据库的输出语言，数据库中没有对应语言时使用英文。该参数可设置的值见 GeoIP2 数据库的支持列表。其他数据库只提供单一语言（例如 IPinfo Lite 为英文）。

```
# NALI_LANG=en NALI_DB_IP4=geoip nali 1.1.1.1
1.1.1.1 [Australia]
```

### 工作目录

设置环境变量 `NALI_HOME` 来指定工作目录，配置文件和数据库存放在工作目录下。也可在配置文件中使用绝对路径指定其他数据库路径。

设置环境变量 `NALI_CONFIG_HOME` 来指定配置文件目录，`NALI_DB_HOME` 来执行数据库文件目录

如果未指定相关环境变量，将使用 XDG 规范，配置文件目录在 `$XDG_CONFIG_HOME/nali`，数据库文件目录在 `$XDG_DATA_HOME/nali`

```
set NALI_HOME=D:\nali

or

export NALI_HOME=/var/nali
```

## 常见问题

### 在管道中间使用时没有颜色

默认只在输出到终端时显示颜色。使用 `--color always` 强制输出颜色，例如 `cmd | nali --color always | less -R`；使用 `--color never` 或设置环境变量 `NO_COLOR=1` 关闭颜色。

### Windows 下中文乱码

在简体中文 Windows（代码页 936）中，nali 会自动把非 UTF-8 的输入按 GBK 解码，`tracert`、`nslookup` 等命令的输出可以直接通过管道交给 nali。在 Windows 控制台（终端）中运行时，nali 会把控制台代码页切换为 UTF-8（65001），并在退出（包括出错退出）时恢复，因此中文输出不再乱码；Windows Terminal 等已经是 UTF-8 的环境不会切换。输入侧的 GBK/GB18030 自动识别保持不变。如果仍然乱码，可以加 `--gbk` 强制按 GBK 解码输入。

### `tail -f log | jq . | nali` 最后几行不输出

nali 读到一行就立即输出。`jq` 的输出不是终端时会缓冲，所以最后几行停留在 jq 中，使用 `jq --unbuffered` 即可：

```sh
tail -f access.log | jq --unbuffered . | nali
```

### 筛选 CDN 结果

域名的 CDN 识别结果在 JSON 输出中 `source` 为 `cdn`，可以用 jq 筛选：

```sh
nali --json < domains.txt | jq -c 'select(.source == "cdn")'
```

## 开发

需要 Go >= 1.26。

```sh
make test      # go test -race ./...
make lint      # go vet + golangci-lint
make all-arch  # 交叉编译所有发布平台
```

测试不依赖网络和真实数据库。MMDB 测试数据位于 `testdata/mmdb`，由独立模块 `testdata/mmdb/generate` 生成，修改后可重新生成：

```sh
cd testdata/mmdb/generate && go run .
```

## 感谢列表

- [纯真QQIP离线数据库](http://www.cz88.net)
- [纯真 CZDB 数据库解析](https://github.com/tagphi/czdb-search-golang)
- [ZX公网ipv6数据库](https://ip.zxinc.org/ipquery/)
- [Geoip2 city数据库](https://www.maxmind.com/en/geoip2-precision-city-service)
- [geoip2-golang解析器](https://github.com/oschwald/geoip2-golang)
- [maxminddb-golang解析器](https://github.com/oschwald/maxminddb-golang)
- [IPinfo Lite数据库](https://ipinfo.io/lite)
- [CDN provider数据库](https://github.com/SukkaLab/cdn)
- [IPIP数据库](https://www.ipip.net/product/ip.html)
- [IPIP数据库解析](https://github.com/ipipdotnet/ipdb-go)
- [ip2region数据库](https://github.com/lionsoul2014/ip2region)
- [IP2Location DB3 LITE](https://lite.ip2location.com/database/db3-ip-country-region-city)
- [Cobra CLI库](https://github.com/spf13/cobra)

感谢 JetBrains 提供开源项目免费License 

<a href="https://www.jetbrains.com/?from=nali">
  <img src="assets/GoLand.svg">
</a>

## 作者

**Nali** © [zu1k](https://github.com/zu1k), 遵循 [MIT](./LICENSE) 证书.<br>
