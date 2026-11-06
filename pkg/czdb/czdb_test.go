package czdb

import (
	"archive/zip"
	"bytes"
	"crypto/aes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	db "github.com/tagphi/czdb-search-golang/pkg/db"
	"github.com/tagphi/czdb-search-golang/pkg/utils"
	"github.com/vmihailenco/msgpack/v5"
)

// testKeyBase64 is the base64 form of the 16 byte AES-128 test key
// "0123456789abcdef". The same bytes are used for the AES encrypted header
// block and for the XOR encrypted geo data.
const testKeyBase64 = "MDEyMzQ1Njc4OWFiY2RlZg=="

func testKey(t *testing.T) []byte {
	t.Helper()
	key, err := base64.StdEncoding.DecodeString(testKeyBase64)
	if err != nil {
		t.Fatalf("decode test key: %v", err)
	}
	return key
}

// segment is one index block of the fixture together with the geo record it
// points at.
type segment struct {
	start, end string   // inclusive IP range
	columns    []string // geo columns; empty means "no geo data" (geoPosMixSize == 0)
	other      string   // otherData appended after the columns
}

// fixture is a generated CZDB file plus the offset of its SuperBlock.
type fixture struct {
	data   []byte
	offset int // file offset of the SuperBlock (= 12 + encryptedBlockSize + randomSize)
}

// buildFixture assembles a CZDB file that follows the layout the search library
// expects:
//
//	HyperHeaderBlock | random padding | SuperBlock | HeaderBlock | index blocks |
//	columnSelection | geoSize | geo data | data records
//
// The header block holds one entry per index block: the first entry is anchored
// at the all-zero IP (the library indexes HeaderPtr[m-1] for a query below the
// first entry, so the first entry must not be above the smallest address), the
// remaining entries start at their segment. Every pointer is relative to the
// SuperBlock offset.
func buildFixture(t *testing.T, ipVersion, randomSize int, columnSelection int32, segments []segment) fixture {
	t.Helper()
	if len(segments) == 0 {
		t.Fatal("fixture needs at least one segment")
	}

	ipLen := 4
	if ipVersion == 6 {
		ipLen = 16
	}
	indexLen := ipLen*2 + indexBlockOverhead

	headerBlockSize := len(segments) * headerBlockEntrySize
	indexAreaStart := superBlockSize + headerBlockSize
	afterIndex := indexAreaStart + len(segments)*indexLen
	geoDataPos := afterIndex + 8 // columnSelection + geoSize

	// Geo blobs (msgpack arrays of strings) and their positions inside the
	// decrypted geo data.
	type geoEntry struct{ offset, length int }
	entries := make([]geoEntry, len(segments))
	geoPlain := make([]byte, 0, 256)
	for i, seg := range segments {
		if len(seg.columns) == 0 {
			continue
		}
		blob, err := msgpack.Marshal(seg.columns)
		if err != nil {
			t.Fatalf("marshal columns: %v", err)
		}
		entries[i] = geoEntry{offset: len(geoPlain), length: len(blob)}
		geoPlain = append(geoPlain, blob...)
	}
	if len(geoPlain) > 0xFFFFFF {
		t.Fatalf("geo data too large: %d", len(geoPlain))
	}

	// Data records: msgpack uint64 geoPosMixSize followed by msgpack string
	// otherData, without an array header.
	records := make([][]byte, len(segments))
	dataPtrs := make([]int, len(segments))
	dataPos := geoDataPos + len(geoPlain)
	for i, seg := range segments {
		mix := uint64(entries[i].length)<<24 | uint64(entries[i].offset)
		var buf bytes.Buffer
		enc := msgpack.NewEncoder(&buf)
		if err := enc.EncodeUint64(mix); err != nil {
			t.Fatalf("encode geoPosMixSize: %v", err)
		}
		if err := enc.EncodeString(seg.other); err != nil {
			t.Fatalf("encode otherData: %v", err)
		}
		records[i] = buf.Bytes()
		if len(records[i]) > 0xFF {
			t.Fatalf("data record too long for the 1 byte data length: %d", len(records[i]))
		}
		dataPtrs[i] = dataPos
		dataPos += len(records[i])
	}
	dbSize := dataPos

	body := make([]byte, 0, dbSize)
	// SuperBlock
	super := make([]byte, superBlockSize)
	if ipVersion == 6 {
		super[0] = 1
	}
	binary.LittleEndian.PutUint32(super[1:], uint32(dbSize))
	binary.LittleEndian.PutUint32(super[5:], uint32(indexAreaStart))
	binary.LittleEndian.PutUint32(super[9:], uint32(headerBlockSize))
	binary.LittleEndian.PutUint32(super[13:], uint32(indexAreaStart+(len(segments)-1)*indexLen))
	body = append(body, super...)

	// HeaderBlock
	for i, seg := range segments {
		entry := make([]byte, headerBlockEntrySize)
		if i > 0 {
			copy(entry, encodeIP(t, seg.start, ipVersion))
		}
		binary.LittleEndian.PutUint32(entry[16:], uint32(indexAreaStart+i*indexLen))
		body = append(body, entry...)
	}

	// Index blocks
	for i, seg := range segments {
		block := make([]byte, indexLen)
		copy(block, encodeIP(t, seg.start, ipVersion))
		copy(block[ipLen:], encodeIP(t, seg.end, ipVersion))
		binary.LittleEndian.PutUint32(block[ipLen*2:], uint32(dataPtrs[i]))
		block[ipLen*2+4] = byte(len(records[i]))
		body = append(body, block...)
	}

	// columnSelection, geoSize and the XOR encrypted geo data
	header := make([]byte, 8)
	binary.LittleEndian.PutUint32(header[0:], uint32(columnSelection))
	binary.LittleEndian.PutUint32(header[4:], uint32(len(geoPlain)))
	body = append(body, header...)
	body = append(body, xorKey(geoPlain, testKey(t))...)

	// Data records
	for _, record := range records {
		body = append(body, record...)
	}
	if len(body) != dbSize {
		t.Fatalf("body size %d, want %d", len(body), dbSize)
	}

	// HyperHeaderBlock + random padding
	encryptedSize := 16
	plain := make([]byte, encryptedSize)
	binary.LittleEndian.PutUint32(plain[0:], uint32(1)<<20|uint32(1)) // clientId << 20 | expirationDate
	binary.LittleEndian.PutUint32(plain[4:], uint32(randomSize))

	file := make([]byte, 0, hyperHeaderFixedSize+encryptedSize+randomSize+len(body))
	hyper := make([]byte, hyperHeaderFixedSize)
	binary.LittleEndian.PutUint32(hyper[0:], 1) // version
	binary.LittleEndian.PutUint32(hyper[4:], 1) // clientId
	binary.LittleEndian.PutUint32(hyper[8:], uint32(encryptedSize))
	file = append(file, hyper...)
	file = append(file, aesECBEncrypt(t, plain)...)
	file = append(file, make([]byte, randomSize)...)
	file = append(file, body...)

	return fixture{data: file, offset: hyperHeaderFixedSize + encryptedSize + randomSize}
}

// encodeIP renders an IP the way the library compares it: 4 bytes plus 12 zero
// bytes for IPv4, 16 bytes for IPv6.
func encodeIP(t *testing.T, ip string, ipVersion int) []byte {
	t.Helper()
	parsed := net.ParseIP(ip)
	if parsed == nil {
		t.Fatalf("invalid fixture IP %q", ip)
	}
	out := make([]byte, 16)
	if ipVersion == 4 {
		v4 := parsed.To4()
		if v4 == nil {
			t.Fatalf("fixture IP %q is not IPv4", ip)
		}
		copy(out, v4)
		return out
	}
	copy(out, parsed.To16())
	return out
}

func aesECBEncrypt(t *testing.T, plain []byte) []byte {
	t.Helper()
	if len(plain)%aes.BlockSize != 0 {
		t.Fatalf("plain text length %d is not a multiple of the AES block size", len(plain))
	}
	block, err := aes.NewCipher(testKey(t))
	if err != nil {
		t.Fatalf("new cipher: %v", err)
	}
	out := make([]byte, len(plain))
	for i := 0; i < len(plain); i += aes.BlockSize {
		block.Encrypt(out[i:i+aes.BlockSize], plain[i:i+aes.BlockSize])
	}
	return out
}

func xorKey(data, key []byte) []byte {
	out := make([]byte, len(data))
	for i := range data {
		out[i] = data[i] ^ key[i%len(key)]
	}
	return out
}

func writeFixture(t *testing.T, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.czdb")
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

// buildZip packs the given members into an in-memory zip archive.
func buildZip(t *testing.T, members []struct {
	name string
	data []byte
}) []byte {
	t.Helper()
	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	for _, member := range members {
		w, err := writer.Create(member.name)
		if err != nil {
			t.Fatalf("create zip member: %v", err)
		}
		if _, err := w.Write(member.data); err != nil {
			t.Fatalf("write zip member: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buf.Bytes()
}

// columnSelectionAll selects the first five columns: column i is selected when
// bit i+1 is set.
const columnSelectionAll = int32(0b111110)

var ipv4Segments = []segment{
	{"1.0.0.0", "1.255.255.255", []string{"中国", "", "浙江省", "杭州市", "电信"}, ""},
	{"2.0.0.0", "2.255.255.255", []string{"美国", "加利福尼亚州", "", "", ""}, "8.8.8.8 示例"},
	{"3.0.0.0", "3.255.255.255", nil, "内网IP  保留"},
}

var ipv6Segments = []segment{
	{"2001:db8::", "2001:db8::ffff", []string{"中国", "北京市", "北京市", "联通", "CN"}, ""},
	{"240e::", "240e::ffff", []string{"中国", "浙江省", "杭州市", "电信", "CN"}, ""},
}

func openFixture(t *testing.T, ipVersion int, segments []segment) *CZDB {
	t.Helper()
	fx := buildFixture(t, ipVersion, 8, columnSelectionAll, segments)
	c, err := New(Options{File: writeFixture(t, fx.data), Key: testKeyBase64})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// useDownloadUrl points the package level DownloadUrl at url for one test.
func useDownloadUrl(t *testing.T, url string) {
	t.Helper()
	previous := DownloadUrl
	DownloadUrl = url
	t.Cleanup(func() { DownloadUrl = previous })
}

// resetPayloadCache drops the process wide archive cache between tests.
func resetPayloadCache(t *testing.T) {
	t.Helper()
	clearPayloadCache()
	t.Cleanup(clearPayloadCache)
}

func clearPayloadCache() {
	payloadMu.Lock()
	payloadCache = map[string][]byte{}
	payloadMu.Unlock()
}

func TestFindIPv4(t *testing.T) {
	c := openFixture(t, 4, ipv4Segments)

	tests := []struct {
		query   string
		want    string
		wantErr bool
	}{
		// hit: tabs become spaces, the empty column is dropped
		{"1.2.3.4", "中国 浙江省 杭州市 电信", false},
		// hit: "null" placeholders are dropped, otherData keeps its spaces
		{"2.2.3.4", "美国 加利福尼亚州 8.8.8.8 示例", false},
		// hit without geo data, runs of spaces collapsed
		{"3.3.3.3", "内网IP 保留", false},
		// IPv4 mapped query is accepted by an IPv4 database
		{"::ffff:1.2.3.4", "中国 浙江省 杭州市 电信", false},
		// misses
		{"0.5.5.5", "", true},
		{"4.4.4.4", "", true},
		// IPv6 query against an IPv4 database
		{"240e::1", "", true},
		// invalid input
		{"not-an-ip", "", true},
		{"1.2.3.4.5", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			got, err := c.Find(tt.query)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Find(%q) error = %v, wantErr %v", tt.query, err, tt.wantErr)
			}
			if err != nil {
				if got != nil {
					t.Fatalf("Find(%q) returned a result with the error: %v", tt.query, got)
				}
				return
			}
			if got.String() != tt.want {
				t.Fatalf("Find(%q) = %q, want %q", tt.query, got.String(), tt.want)
			}
		})
	}
}

func TestFindIPv6(t *testing.T) {
	c := openFixture(t, 6, ipv6Segments)

	tests := []struct {
		query   string
		want    string
		wantErr bool
	}{
		{"2001:db8::1", "中国 北京市 北京市 联通 CN", false},
		{"240e::1", "中国 浙江省 杭州市 电信 CN", false},
		{"240f::1", "", true},
		{"1.1.1.1", "", true},
		{"::ffff:1.2.3.4", "", true},
		{"", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			got, err := c.Find(tt.query)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Find(%q) error = %v, wantErr %v", tt.query, err, tt.wantErr)
			}
			if err == nil && got.String() != tt.want {
				t.Fatalf("Find(%q) = %q, want %q", tt.query, got.String(), tt.want)
			}
		})
	}
}

// TestFixtureMatchesLibrary checks the generated fixture against the library
// itself, so the fixture layout stays honest even if the wrapper changes.
func TestFixtureMatchesLibrary(t *testing.T) {
	fx := buildFixture(t, 4, 8, columnSelectionAll, ipv4Segments)
	searcher, err := db.InitDBSearcher(writeFixture(t, fx.data), testKeyBase64, db.MEMORY)
	if err != nil {
		t.Fatalf("InitDBSearcher: %v", err)
	}
	defer db.CloseDBSearcher(searcher)

	raw, err := db.Search("1.2.3.4", searcher)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if want := "中国\tnull\t浙江省\t杭州市\t电信\t"; raw != want {
		t.Fatalf("raw result = %q, want %q", raw, want)
	}
	if got := formatResult(raw); got != "中国 浙江省 杭州市 电信" {
		t.Fatalf("formatResult = %q", got)
	}

	if raw, err := db.Search("4.4.4.4", searcher); err != nil || raw != notFoundSentinel {
		t.Fatalf("miss = %q, %v, want %q", raw, err, notFoundSentinel)
	}
}

func TestFormatResult(t *testing.T) {
	tests := map[string]string{
		"":                   "",
		"中国\tnull\t浙江省\t":    "中国 浙江省",
		"a\tb\tc\t":          "a b c",
		"中国  \t 浙江省\t杭州市":    "中国 浙江省 杭州市",
		"null\tnull\t":       "",
		"美国\t\t\t8.8.8.8 示例": "美国 8.8.8.8 示例",
		" 中国 \t 浙江省 ":        "中国 浙江省",
	}
	for raw, want := range tests {
		if got := formatResult(raw); got != want {
			t.Errorf("formatResult(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestCheckFile(t *testing.T) {
	v4 := buildFixture(t, 4, 8, columnSelectionAll, ipv4Segments)
	v6 := buildFixture(t, 6, 0, columnSelectionAll, ipv6Segments)
	bigPadding := buildFixture(t, 4, 512, columnSelectionAll, ipv4Segments)

	for name, fx := range map[string]fixture{"ipv4": v4, "ipv6": v6, "padding": bigPadding} {
		if !CheckFile(fx.data) {
			t.Errorf("%s: valid file rejected", name)
		}
		for _, cut := range []int{1, 10, 100} {
			if CheckFile(fx.data[:len(fx.data)-cut]) {
				t.Errorf("%s: file truncated by %d bytes accepted", name, cut)
			}
		}
	}

	for name, data := range map[string][]byte{
		"nil":           nil,
		"empty":         {},
		"html":          []byte("<!DOCTYPE html><html><body>404 Not Found</body></html>"),
		"short":         []byte("CZDB"),
		"zeros":         make([]byte, 4096),
		"random prefix": append([]byte("<?xml version=\"1.0\"?><Error>"), make([]byte, 1024)...),
		"json error":    []byte(`{"code":5101,"success":false,"message":"非法访问"}`),
	} {
		if CheckFile(data) {
			t.Errorf("%s: invalid file accepted", name)
		}
	}
}

func TestCheckFileRejectsBrokenSuperBlock(t *testing.T) {
	fx := buildFixture(t, 4, 8, columnSelectionAll, ipv4Segments)
	off := fx.offset

	mutations := map[string]func([]byte){
		"dbType": func(d []byte) { d[off] = 2 },
		"dbSize": func(d []byte) {
			binary.LittleEndian.PutUint32(d[off+1:], uint32(len(d)-off+1))
		},
		"headerBlockSize not a multiple of 20": func(d []byte) {
			binary.LittleEndian.PutUint32(d[off+9:], 21)
		},
		"headerBlockSize zero": func(d []byte) {
			binary.LittleEndian.PutUint32(d[off+9:], 0)
		},
		"headerBlockSize too large": func(d []byte) {
			binary.LittleEndian.PutUint32(d[off+9:], uint32(len(d)))
		},
		"startIndexPtr after endIndexPtr": func(d []byte) {
			binary.LittleEndian.PutUint32(d[off+5:], 1<<20)
		},
		"endIndexPtr out of range": func(d []byte) {
			binary.LittleEndian.PutUint32(d[off+13:], uint32(len(d)))
		},
		"encryptedBlockSize zero": func(d []byte) {
			binary.LittleEndian.PutUint32(d[8:], 0)
		},
		"encryptedBlockSize huge": func(d []byte) {
			binary.LittleEndian.PutUint32(d[8:], maxEncryptedBlockSize+1)
		},
	}
	for name, mutate := range mutations {
		data := bytes.Clone(fx.data)
		mutate(data)
		if CheckFile(data) {
			t.Errorf("%s: broken file accepted", name)
		}
	}
}

// TestCheckFileFor makes sure the family check rejects a database of the other
// family, which is what keeps a mixed up download from silently disabling a
// whole address family.
func TestCheckFileFor(t *testing.T) {
	v4 := buildFixture(t, 4, 8, columnSelectionAll, ipv4Segments)
	v6 := buildFixture(t, 6, 0, columnSelectionAll, ipv6Segments)

	if !CheckFileFor(v4.data, false) {
		t.Error("IPv4 database rejected as IPv4")
	}
	if CheckFileFor(v4.data, true) {
		t.Error("IPv4 database accepted as IPv6")
	}
	if !CheckFileFor(v6.data, true) {
		t.Error("IPv6 database rejected as IPv6")
	}
	if CheckFileFor(v6.data, false) {
		t.Error("IPv6 database accepted as IPv4")
	}

	for name, data := range map[string][]byte{
		"nil":     nil,
		"empty":   {},
		"html":    []byte("<!DOCTYPE html><html><body>404 Not Found</body></html>"),
		"zeros":   make([]byte, 4096),
		"v4 cut":  v4.data[:len(v4.data)-10],
		"v6 cut":  v6.data[:len(v6.data)-10],
		"v4 json": []byte(`{"code":5101,"success":false,"message":"非法访问"}`),
	} {
		if CheckFileFor(data, false) || CheckFileFor(data, true) {
			t.Errorf("%s: invalid file accepted", name)
		}
	}
}

func TestExpandURLs(t *testing.T) {
	t.Setenv("CZDB_DOWNLOAD_KEY", "")
	t.Setenv("NALI_CZDB_DOWNLOAD_KEY", "")

	urls := []string{"https://example.com/db?key={key}", "https://example.com/plain"}
	if _, err := ExpandURLs(urls); err == nil {
		t.Fatal("placeholder without a download key should fail")
	}

	if got, err := ExpandURLs(urls[1:]); err != nil || len(got) != 1 || got[0] != urls[1] {
		t.Fatalf("ExpandURLs without placeholder = %v, %v", got, err)
	}

	t.Setenv("NALI_CZDB_DOWNLOAD_KEY", "fallback-key")
	got, err := ExpandURLs(urls)
	if err != nil {
		t.Fatalf("ExpandURLs: %v", err)
	}
	if want := "https://example.com/db?key=fallback-key"; got[0] != want {
		t.Fatalf("expanded url = %q, want %q", got[0], want)
	}

	t.Setenv("CZDB_DOWNLOAD_KEY", "primary-key")
	got, err = ExpandURLs(urls)
	if err != nil {
		t.Fatalf("ExpandURLs: %v", err)
	}
	if want := "https://example.com/db?key=primary-key"; got[0] != want {
		t.Fatalf("expanded url = %q, want %q", got[0], want)
	}
}

func TestResolveKey(t *testing.T) {
	t.Setenv("CZDB_KEY", "")
	t.Setenv("NALI_CZDB_KEY", "")

	if got := ResolveKey("configured"); got != "configured" {
		t.Fatalf("ResolveKey(configured) = %q", got)
	}
	if got := ResolveKey(""); got != "" {
		t.Fatalf("ResolveKey with no environment = %q", got)
	}
	t.Setenv("NALI_CZDB_KEY", "fallback")
	if got := ResolveKey(""); got != "fallback" {
		t.Fatalf("ResolveKey fallback = %q", got)
	}
	t.Setenv("CZDB_KEY", "primary")
	if got := ResolveKey(""); got != "primary" {
		t.Fatalf("ResolveKey primary = %q", got)
	}
	if got := ResolveKey("configured"); got != "configured" {
		t.Fatalf("ResolveKey(configured) = %q", got)
	}
}

func TestResolveDownloadKey(t *testing.T) {
	t.Setenv("CZDB_DOWNLOAD_KEY", "")
	t.Setenv("NALI_CZDB_DOWNLOAD_KEY", "")
	if got := ResolveDownloadKey(); got != "" {
		t.Fatalf("ResolveDownloadKey = %q", got)
	}
	t.Setenv("NALI_CZDB_DOWNLOAD_KEY", "fallback")
	if got := ResolveDownloadKey(); got != "fallback" {
		t.Fatalf("ResolveDownloadKey fallback = %q", got)
	}
	t.Setenv("CZDB_DOWNLOAD_KEY", "primary")
	if got := ResolveDownloadKey(); got != "primary" {
		t.Fatalf("ResolveDownloadKey primary = %q", got)
	}
}

func TestNewRequiresKey(t *testing.T) {
	t.Setenv("CZDB_KEY", "")
	t.Setenv("NALI_CZDB_KEY", "")

	_, err := New(Options{File: filepath.Join(t.TempDir(), "missing.czdb")})
	if err == nil || !strings.Contains(err.Error(), "CZDB_KEY") {
		t.Fatalf("error = %v, want a hint about CZDB_KEY", err)
	}
}

func TestNewRequiresFile(t *testing.T) {
	if _, err := New(Options{Key: testKeyBase64}); err == nil {
		t.Fatal("missing file path should fail")
	}
}

func TestNewMissingFileWithoutDownloadURL(t *testing.T) {
	useDownloadUrl(t, "")

	_, err := New(Options{File: filepath.Join(t.TempDir(), "missing.czdb"), Key: testKeyBase64})
	if err == nil || !strings.Contains(err.Error(), "下载地址") {
		t.Fatalf("error = %v, want a hint about the download url", err)
	}
}

func TestNewInvalidFile(t *testing.T) {
	path := writeFixture(t, []byte("<html>not a database</html>"))
	if _, err := New(Options{File: path, Key: testKeyBase64}); err == nil {
		t.Fatal("invalid file should fail")
	}
}

func TestNewWrongKey(t *testing.T) {
	fx := buildFixture(t, 4, 8, columnSelectionAll, ipv4Segments)
	path := writeFixture(t, fx.data)
	wrong := base64.StdEncoding.EncodeToString([]byte("fedcba9876543210"))
	if _, err := New(Options{File: path, Key: wrong}); err == nil {
		t.Fatal("wrong key should fail")
	}
}

// TestNewRejectsTruncatedFile makes sure a file that is cut right after the
// index area is rejected instead of being opened as a database that never
// answers.
func TestNewRejectsTruncatedFile(t *testing.T) {
	fx := buildFixture(t, 4, 8, columnSelectionAll, ipv4Segments)
	indexLen := 4*2 + indexBlockOverhead
	cut := fx.offset + superBlockSize +
		len(ipv4Segments)*headerBlockEntrySize + len(ipv4Segments)*indexLen
	if cut >= len(fx.data) {
		t.Fatalf("bad fixture layout: cut %d, length %d", cut, len(fx.data))
	}
	if _, err := New(Options{File: writeFixture(t, fx.data[:cut]), Key: testKeyBase64}); err == nil {
		t.Fatal("truncated database should fail")
	} else {
		t.Logf("New 返回错误: %v", err)
	}
}

// TestNewRejectsUnloadableDatabase makes sure a structurally valid file whose
// content is broken is reported instead of silently answering "not found".
func TestNewRejectsUnloadableDatabase(t *testing.T) {
	fx := buildFixture(t, 4, 8, columnSelectionAll, ipv4Segments)
	data := bytes.Clone(fx.data)
	// Point the first header entry past the end of the database body, which
	// leaves the structure intact but makes every lookup fail.
	binary.LittleEndian.PutUint32(data[fx.offset+superBlockSize+16:], uint32(len(data)))
	if !CheckFile(data) {
		t.Fatal("the corrupted fixture should still look structurally valid")
	}

	_, err := New(Options{File: writeFixture(t, data), Key: testKeyBase64})
	if err == nil {
		t.Fatal("unloadable database should fail")
	}
	if !strings.Contains(err.Error(), "无法载入 czdb 数据库") {
		t.Fatalf("error = %v, want a load failure", err)
	}
}

// TestNewDownloadsZip covers the official endpoint: one zip with both database
// files, the requested entry extracted and the {key} placeholder expanded.
func TestNewDownloadsZip(t *testing.T) {
	v4 := buildFixture(t, 4, 8, columnSelectionAll, ipv4Segments)
	v6 := buildFixture(t, 6, 8, columnSelectionAll, ipv6Segments)
	archive := buildZip(t, []struct {
		name string
		data []byte
	}{
		{EntryIPv4, v4.data},
		{EntryIPv6, v6.data},
	})

	var requests atomic.Int32
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/zip")
		_, _ = w.Write(archive)
	}))
	defer srv.Close()

	t.Setenv("CZDB_DOWNLOAD_KEY", "secret-key")
	useDownloadUrl(t, srv.URL+"/api/communityIpDbFile?fn=czdb&key={key}")

	path := filepath.Join(t.TempDir(), EntryIPv6)
	c, err := New(Options{File: path, Key: testKeyBase64, ZipEntry: EntryIPv6})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer c.Close()

	if got := gotQuery; got != "fn=czdb&key=secret-key" {
		t.Fatalf("request query = %q", got)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("requests = %d, want 1", got)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("downloaded file missing: %v", err)
	}
	got, err := c.Find("240e::1")
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if want := "中国 浙江省 杭州市 电信 CN"; got.String() != want {
		t.Fatalf("Find = %q, want %q", got.String(), want)
	}
}

func TestNewDownloadsZipMissingEntry(t *testing.T) {
	v4 := buildFixture(t, 4, 8, columnSelectionAll, ipv4Segments)
	archive := buildZip(t, []struct {
		name string
		data []byte
	}{{EntryIPv4, v4.data}})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(archive)
	}))
	defer srv.Close()

	path := filepath.Join(t.TempDir(), EntryIPv6)
	_, err := New(Options{File: path, Key: testKeyBase64, ZipEntry: EntryIPv6, DownloadUrls: []string{srv.URL}})
	if err == nil {
		t.Fatal("missing zip entry should fail")
	}
	if !strings.Contains(err.Error(), EntryIPv6) || !strings.Contains(err.Error(), EntryIPv4) {
		t.Fatalf("error = %v, want the requested and the available entry names", err)
	}
}

// TestNewDownloadsBareDatabase covers a custom mirror that serves a raw czdb
// file instead of the official archive.
func TestNewDownloadsBareDatabase(t *testing.T) {
	fx := buildFixture(t, 4, 8, columnSelectionAll, ipv4Segments)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(fx.data)
	}))
	defer srv.Close()

	path := filepath.Join(t.TempDir(), EntryIPv4)
	c, err := New(Options{File: path, Key: testKeyBase64, DownloadUrls: []string{srv.URL}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer c.Close()

	got, err := c.Find("1.2.3.4")
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if want := "中国 浙江省 杭州市 电信"; got.String() != want {
		t.Fatalf("Find = %q, want %q", got.String(), want)
	}
}

// TestNewDownloadJSONError covers the endpoint answering HTTP 200 with a JSON
// error body when the download key is invalid.
func TestNewDownloadJSONError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":5101,"success":false,"message":"非法访问"}`))
	}))
	defer srv.Close()

	path := filepath.Join(t.TempDir(), EntryIPv4)
	_, err := New(Options{File: path, Key: testKeyBase64, DownloadUrls: []string{srv.URL}})
	if err == nil {
		t.Fatal("json error response should fail")
	}
	if !strings.Contains(err.Error(), "非法访问") {
		t.Fatalf("error = %v, want the message of the json body", err)
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("invalid download was written to disk: %v", statErr)
	}
}

func TestNewDownloadRetries(t *testing.T) {
	v4 := buildFixture(t, 4, 8, columnSelectionAll, ipv4Segments)
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) < downloadRetries {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		_, _ = w.Write(v4.data)
	}))
	defer srv.Close()

	path := filepath.Join(t.TempDir(), EntryIPv4)
	c, err := New(Options{File: path, Key: testKeyBase64, DownloadUrls: []string{srv.URL}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer c.Close()

	if got := requests.Load(); got != downloadRetries {
		t.Fatalf("requests = %d, want %d", got, downloadRetries)
	}
}

func TestNewDownloadGivesUp(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.NotFound(w, r)
	}))
	defer srv.Close()

	path := filepath.Join(t.TempDir(), EntryIPv4)
	_, err := New(Options{File: path, Key: testKeyBase64, DownloadUrls: []string{srv.URL}})
	if err == nil || !strings.Contains(err.Error(), "下载失败") {
		t.Fatalf("error = %v, want a download failure", err)
	}
	if got, want := requests.Load(), int32(downloadRetries+1); got != want {
		t.Fatalf("requests = %d, want %d", got, want)
	}
}

// TestDownloadFailureRedactsKey makes sure the download key, which travels in
// the query string of the download URL, never ends up in an error message (and
// therefore in nali's log output).
func TestDownloadFailureRedactsKey(t *testing.T) {
	const secret = "very-secret-download-key"
	for _, status := range []int{http.StatusInternalServerError, http.StatusForbidden} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			resetPayloadCache(t)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
			}))
			defer srv.Close()

			t.Setenv("CZDB_DOWNLOAD_KEY", secret)
			t.Setenv("NALI_CZDB_DOWNLOAD_KEY", "")
			useDownloadUrl(t, srv.URL+"/api/communityIpDbFile?fn=czdb&key={key}")

			_, err := FetchEntry(EntryIPv4, nil)
			if err == nil {
				t.Fatal("a failed download should fail")
			}
			if strings.Contains(err.Error(), secret) {
				t.Fatalf("error leaks the download key: %v", err)
			}
			if !strings.Contains(err.Error(), "***") {
				t.Fatalf("error = %v, want the key replaced by ***", err)
			}
		})
	}
}

func TestFetchEntryCachesArchive(t *testing.T) {
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

	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		_, _ = w.Write(archive)
	}))
	defer srv.Close()
	useDownloadUrl(t, srv.URL)

	got4, err := FetchEntry(EntryIPv4, nil)
	if err != nil {
		t.Fatalf("FetchEntry(%s): %v", EntryIPv4, err)
	}
	if !bytes.Equal(got4, v4.data) {
		t.Fatal("ipv4 entry mismatch")
	}
	if !CheckFileFor(got4, false) {
		t.Fatal("the ipv4 entry is not an IPv4 database")
	}
	got6, err := FetchEntry(EntryIPv6, nil)
	if err != nil {
		t.Fatalf("FetchEntry(%s): %v", EntryIPv6, err)
	}
	if !bytes.Equal(got6, v6.data) {
		t.Fatal("ipv6 entry mismatch")
	}
	if !CheckFileFor(got6, true) {
		t.Fatal("the ipv6 entry is not an IPv6 database")
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("requests = %d, want 1 (archive cached)", got)
	}

	// The empty entry name defaults to the IPv4 database.
	if got, err := FetchEntry("", nil); err != nil || !bytes.Equal(got, v4.data) {
		t.Fatalf("FetchEntry(\"\") = %d bytes, %v", len(got), err)
	}
}

// TestFetchEntryUsesCustomUrls checks that a configured download source is used
// instead of the official endpoint, and that every source is cached separately.
func TestFetchEntryUsesCustomUrls(t *testing.T) {
	resetPayloadCache(t)
	v4 := buildFixture(t, 4, 8, columnSelectionAll, ipv4Segments)
	archive := buildZip(t, []struct {
		name string
		data []byte
	}{{EntryIPv4, v4.data}})

	var official, custom atomic.Int32
	officialSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		official.Add(1)
		http.NotFound(w, r)
	}))
	defer officialSrv.Close()
	customSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		custom.Add(1)
		_, _ = w.Write(archive)
	}))
	defer customSrv.Close()
	useDownloadUrl(t, officialSrv.URL)

	for i := 0; i < 2; i++ {
		got, err := FetchEntry(EntryIPv4, []string{customSrv.URL})
		if err != nil {
			t.Fatalf("FetchEntry with custom urls: %v", err)
		}
		if !bytes.Equal(got, v4.data) {
			t.Fatal("entry mismatch")
		}
	}
	if got := official.Load(); got != 0 {
		t.Fatalf("official endpoint requests = %d, want 0", got)
	}
	if got := custom.Load(); got != 1 {
		t.Fatalf("custom endpoint requests = %d, want 1", got)
	}

	// A second source has its own cache entry, so it downloads once more.
	if _, err := FetchEntry(EntryIPv4, []string{customSrv.URL + "/mirror"}); err != nil {
		t.Fatalf("FetchEntry with a second source: %v", err)
	}
	if got := custom.Load(); got != 2 {
		t.Fatalf("custom endpoint requests = %d, want 2", got)
	}
}

// TestFetchEntryBareDatabase covers a custom mirror that serves a raw czdb file
// instead of an archive.
func TestFetchEntryBareDatabase(t *testing.T) {
	resetPayloadCache(t)
	fx := buildFixture(t, 4, 8, columnSelectionAll, ipv4Segments)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(fx.data)
	}))
	defer srv.Close()

	got, err := FetchEntry(EntryIPv4, []string{srv.URL})
	if err != nil {
		t.Fatalf("FetchEntry: %v", err)
	}
	if !bytes.Equal(got, fx.data) {
		t.Fatal("bare database mismatch")
	}
}

// TestFetchEntryRejectsWrongFamily makes sure a bare database of the wrong
// family is refused instead of being stored under the name of the other
// family, where every query of that family would look like a plain miss.
func TestFetchEntryRejectsWrongFamily(t *testing.T) {
	resetPayloadCache(t)
	v4 := buildFixture(t, 4, 8, columnSelectionAll, ipv4Segments)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(v4.data)
	}))
	defer srv.Close()

	_, err := FetchEntry(EntryIPv6, []string{srv.URL})
	if err == nil {
		t.Fatal("an IPv4 database must not be accepted as IPv6")
	}
	for _, want := range []string{"IPv6", EntryIPv6, "zip-entry"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %v, want it to mention %q", err, want)
		}
	}

	// The same source still serves the IPv4 database it actually holds.
	got, err := FetchEntry(EntryIPv4, []string{srv.URL})
	if err != nil {
		t.Fatalf("FetchEntry(%s): %v", EntryIPv4, err)
	}
	if !bytes.Equal(got, v4.data) {
		t.Fatal("bare database mismatch")
	}
}

// TestFetchEntryRejectsWrongFamilyInZip covers the same mix up inside the
// archive: the member named for the IPv6 database holds an IPv4 database.
func TestFetchEntryRejectsWrongFamilyInZip(t *testing.T) {
	resetPayloadCache(t)
	v4 := buildFixture(t, 4, 8, columnSelectionAll, ipv4Segments)
	archive := buildZip(t, []struct {
		name string
		data []byte
	}{
		{EntryIPv4, v4.data},
		{EntryIPv6, v4.data},
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(archive)
	}))
	defer srv.Close()

	if _, err := FetchEntry(EntryIPv6, []string{srv.URL}); err == nil {
		t.Fatal("an IPv4 database must not be accepted as IPv6")
	} else if !strings.Contains(err.Error(), "IPv6") {
		t.Fatalf("error = %v, want it to mention IPv6", err)
	}
	if got, err := FetchEntry(EntryIPv4, []string{srv.URL}); err != nil || !bytes.Equal(got, v4.data) {
		t.Fatalf("FetchEntry(%s) = %d bytes, %v", EntryIPv4, len(got), err)
	}
}

// TestFetchEntryDoesNotCacheErrors makes sure a failed download is retried
// instead of being remembered.
func TestFetchEntryDoesNotCacheErrors(t *testing.T) {
	resetPayloadCache(t)
	v4 := buildFixture(t, 4, 8, columnSelectionAll, ipv4Segments)
	archive := buildZip(t, []struct {
		name string
		data []byte
	}{{EntryIPv4, v4.data}})

	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			_, _ = w.Write([]byte(`{"code":5101,"success":false,"message":"非法访问"}`))
			return
		}
		_, _ = w.Write(archive)
	}))
	defer srv.Close()
	useDownloadUrl(t, srv.URL)

	if _, err := FetchEntry(EntryIPv4, nil); err == nil || !strings.Contains(err.Error(), "非法访问") {
		t.Fatalf("first FetchEntry error = %v", err)
	}
	if _, err := FetchEntry(EntryIPv4, nil); err != nil {
		t.Fatalf("second FetchEntry: %v", err)
	}
	if got := requests.Load(); got != 2 {
		t.Fatalf("requests = %d, want 2", got)
	}
}

func TestFetchEntryRequiresDownloadKey(t *testing.T) {
	resetPayloadCache(t)
	t.Setenv("CZDB_DOWNLOAD_KEY", "")
	t.Setenv("NALI_CZDB_DOWNLOAD_KEY", "")
	useDownloadUrl(t, "http://127.0.0.1:1/api?fn=czdb&key={key}")

	if _, err := FetchEntry(EntryIPv4, nil); err == nil || !strings.Contains(err.Error(), "CZDB_DOWNLOAD_KEY") {
		t.Fatalf("error = %v, want a hint about CZDB_DOWNLOAD_KEY", err)
	}
}

func TestCloseIsIdempotent(t *testing.T) {
	c := openFixture(t, 4, ipv4Segments)
	if err := c.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if _, err := c.Find("1.2.3.4"); err == nil {
		t.Fatal("Find after Close should fail")
	}
}

func TestName(t *testing.T) {
	c := openFixture(t, 4, ipv4Segments)
	if got := c.Name(); got != "czdb" {
		t.Fatalf("Name() = %q", got)
	}
}

// TestDebugOutputIsStderr keeps the search library from writing its warnings to
// stdout, which would corrupt nali's piped and --json output.
func TestDebugOutputIsStderr(t *testing.T) {
	if utils.DebugOutput != os.Stderr {
		t.Fatalf("utils.DebugOutput = %T, want os.Stderr", utils.DebugOutput)
	}
}

func TestConcurrentFind(t *testing.T) {
	c := openFixture(t, 4, ipv4Segments)

	const goroutines = 32
	const iterations = 20
	want := map[string]string{
		"1.2.3.4": "中国 浙江省 杭州市 电信",
		"2.2.3.4": "美国 加利福尼亚州 8.8.8.8 示例",
		"3.3.3.3": "内网IP 保留",
	}

	var wg sync.WaitGroup
	errs := make(chan error, goroutines)
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				for query, expected := range want {
					got, err := c.Find(query)
					if err != nil {
						errs <- err
						return
					}
					if got.String() != expected {
						errs <- fmt.Errorf("unexpected result for %s: %s", query, got.String())
						return
					}
				}
				if _, err := c.Find("4.4.4.4"); err == nil {
					errs <- errors.New("miss should return an error")
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
}

// TestRealDatabase runs against a real database file and is skipped unless
// CZDB_TEST_DB_PATH and CZDB_TEST_DB_KEY are set. CZDB_TEST_DB_QUERY and
// CZDB_TEST_DB_WANT optionally select another address and its expected answer
// (for example the IPv6 database).
func TestRealDatabase(t *testing.T) {
	path := os.Getenv("CZDB_TEST_DB_PATH")
	key := os.Getenv("CZDB_TEST_DB_KEY")
	if path == "" || key == "" {
		t.Skip("设置 CZDB_TEST_DB_PATH 与 CZDB_TEST_DB_KEY 后运行真实数据库测试")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if !CheckFile(data) {
		t.Fatalf("CheckFile 拒绝了真实 czdb 文件 %s", path)
	}

	c, err := New(Options{File: path, Key: key})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer c.Close()

	query := os.Getenv("CZDB_TEST_DB_QUERY")
	if query == "" {
		query = "8.8.8.8"
	}
	want := os.Getenv("CZDB_TEST_DB_WANT")
	if want == "" {
		want = "美国–加利福尼亚州–圣克拉拉–山景城 谷歌公司DNS服务器"
	}

	result, err := c.Find(query)
	if err != nil {
		t.Fatalf("Find(%q): %v", query, err)
	}
	got := result.String()
	if got != want {
		// A newer database may answer differently; the normalization contract
		// is what really has to hold.
		t.Logf("Find(%q) = %q, want %q（真实数据版本可能不同）", query, got, want)
	}
	if got == "" || strings.ContainsAny(got, "\t") {
		t.Fatalf("结果未规范化: %q", got)
	}
	t.Logf("Find(%q) = %q", query, got)
}
