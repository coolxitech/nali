package migration

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/viper"

	"github.com/coolxitech/nali/internal/constant"
	"github.com/coolxitech/nali/internal/db"
	"github.com/coolxitech/nali/pkg/czdb"
	"github.com/coolxitech/nali/pkg/ip2region"
)

// withConfig points the migrations at a temporary config directory holding
// the given config.yaml (or none, if content is empty).
func withConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	if content != "" {
		if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	old := constant.ConfigDirPath
	constant.ConfigDirPath = dir
	viper.Reset()
	t.Cleanup(func() {
		constant.ConfigDirPath = old
		viper.Reset()
	})
	return dir
}

func readConfig(t *testing.T, dir string) *viper.Viper {
	t.Helper()
	v := viper.New()
	v.SetConfigFile(filepath.Join(dir, "config.yaml"))
	if err := v.ReadInConfig(); err != nil {
		t.Fatal(err)
	}
	return v
}

// readDBList returns the databases of the config file in file order.
func readDBList(t *testing.T, dir string) db.List {
	t.Helper()
	var list db.List
	if err := readConfig(t, dir).UnmarshalKey("databases", &list); err != nil {
		t.Fatal(err)
	}
	return list
}

func readDBs(t *testing.T, dir string) map[string]*db.DB {
	t.Helper()
	dbs := make(map[string]*db.DB)
	for _, d := range readDBList(t, dir) {
		dbs[d.Name] = d
	}
	return dbs
}

// dbNames returns the database names in config file order.
func dbNames(list db.List) []string {
	names := make([]string, 0, len(list))
	for _, d := range list {
		names = append(names, d.Name)
	}
	return names
}

func readString(t *testing.T, dir, key string) string {
	t.Helper()
	return readConfig(t, dir).GetString(key)
}

// staleIP2RegionConfig holds an ip2region entry with the outdated download URL
// and settings the migrations must not touch.
const staleIP2RegionConfig = `databases:
- name: ip2region
  name-alias: [i2r]
  format: ip2region
  file: ip2region.xdb
  languages: [zh-CN]
  types: [IPv4]
  download-urls:
  - https://cdn.jsdelivr.net/gh/lionsoul2014/ip2region/data/ip2region.xdb
  - https://raw.githubusercontent.com/lionsoul2014/ip2region/master/data/ip2region.xdb
- name: cdn
  format: cdn-yml
  file: cdn.yml
  languages: [zh-CN]
  types: [CDN]
  download-urls:
  - https://example.com/my-cdn.yml
selected:
  ipv4: ip2region
  ipv6: zxipv6wry
`

func TestMigrationsUpdateStaleDownloadURLs(t *testing.T) {
	dir := withConfig(t, staleIP2RegionConfig)

	migration2v8()

	dbs := readDBs(t, dir)
	if got := dbs["ip2region"].DownloadUrls; !slices.Equal(got, ip2region.DownloadUrls) {
		t.Errorf("ip2region urls not migrated: %v", got)
	}
	// unrelated entries and settings are preserved
	if got := dbs["cdn"].DownloadUrls; !slices.Equal(got, []string{"https://example.com/my-cdn.yml"}) {
		t.Errorf("cdn urls changed: %v", got)
	}
	if got := dbs["ip2region"].NameAlias; !slices.Equal(got, []string{"i2r"}) {
		t.Errorf("ip2region alias changed: %v", got)
	}
	if got := readString(t, dir, "selected.ipv4"); got != "ip2region" {
		t.Errorf("selected.ipv4 changed to %q", got)
	}
}

func TestMigrationsKeepCustomDownloadURLs(t *testing.T) {
	const custom = `databases:
- name: ip2region
  format: ip2region
  file: ip2region.xdb
  download-urls: [https://mirror.example.com/ip2region_v4.xdb]
`
	dir := withConfig(t, custom)
	before, err := os.ReadFile(filepath.Join(dir, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	migration2v8()

	after, err := os.ReadFile(filepath.Join(dir, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatalf("config rewritten although nothing needed migrating:\n%s", after)
	}
}

func TestMigrationsWithoutConfig(t *testing.T) {
	dir := withConfig(t, "")

	migration2v8()
	migration2v9()

	if _, err := os.Stat(filepath.Join(dir, "config.yaml")); !os.IsNotExist(err) {
		t.Fatalf("migrations must not create a config file: %v", err)
	}
}

// oldQQWryConfig is a config written by a version still using the retired
// qqwry database.
const oldQQWryConfig = `databases:
- name: qqwry
  name-alias: [chunzhen]
  format: qqwry
  file: qqwry.dat
  languages: [zh-CN]
  types: [IPv4]
  download-urls:
  - https://mirror.example.com/qqwry.dat
- name: ip2region
  name-alias: [i2r]
  format: ip2region
  file: ip2region.xdb
  download-urls:
  - https://mirror.example.com/ip2region_v4.xdb
- name: cdn
  format: cdn-yml
  file: cdn.yml
  download-urls:
  - https://example.com/my-cdn.yml
selected:
  ipv4: qqwry
  ipv6: zxipv6wry
`

// checkDefaultCZDBEntry asserts that adb is the built-in czdb definition.
func checkDefaultCZDBEntry(t *testing.T, adb *db.DB) {
	t.Helper()
	if adb == nil {
		t.Fatal("czdb entry missing")
	}
	if got := adb.Name; got != "czdb" {
		t.Errorf("czdb entry name = %q", got)
	}
	if got := adb.Format; got != db.FormatCZDB {
		t.Errorf("czdb entry format = %q", got)
	}
	if got := adb.File; got != "ipv4.czdb" {
		t.Errorf("czdb entry file = %q", got)
	}
	if got := adb.ZipEntry; got != czdb.EntryIPv4 {
		t.Errorf("czdb entry zip-entry = %q", got)
	}
	for _, alias := range []string{"chunzhen", "cz88"} {
		if !slices.Contains(adb.NameAlias, alias) {
			t.Errorf("czdb entry alias %q missing: %v", alias, adb.NameAlias)
		}
	}
	if len(adb.DownloadUrls) == 0 || adb.DownloadUrls[0] != czdb.DownloadUrl {
		t.Errorf("czdb entry download-urls = %v", adb.DownloadUrls)
	}
	if !slices.Contains(adb.Types, db.TypeIPv4) {
		t.Errorf("czdb entry types = %v", adb.Types)
	}
}

func TestMigration2v9ReplacesOldQQWry(t *testing.T) {
	dir := withConfig(t, oldQQWryConfig)

	migration2v9()

	// the qqwry entry is replaced in place by the czdb definition, the other
	// entries keep their position and settings
	list := readDBList(t, dir)
	if got, want := dbNames(list), []string{"czdb", "ip2region", "cdn"}; !slices.Equal(got, want) {
		t.Fatalf("databases after migration = %v, want %v", got, want)
	}
	checkDefaultCZDBEntry(t, list[0])

	dbs := readDBs(t, dir)
	if _, found := dbs["ip2region"]; !found {
		t.Errorf("ip2region entry lost: %v", dbs)
	}
	// the remaining entries keep their own settings
	if got := dbs["ip2region"].DownloadUrls; !slices.Equal(got, []string{"https://mirror.example.com/ip2region_v4.xdb"}) {
		t.Errorf("ip2region urls changed: %v", got)
	}
	if got := dbs["cdn"].DownloadUrls; !slices.Equal(got, []string{"https://example.com/my-cdn.yml"}) {
		t.Errorf("cdn urls changed: %v", got)
	}
	if got := readString(t, dir, "selected.ipv4"); got != "czdb" {
		t.Errorf("selected.ipv4 changed to %q", got)
	}
	if got := readString(t, dir, "selected.ipv6"); got != "zxipv6wry" {
		t.Errorf("selected.ipv6 changed to %q", got)
	}

	// Rewriting the config must not inject empty lists into entries that never
	// had languages or types.
	raw, err := os.ReadFile(filepath.Join(dir, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, unwanted := range []string{"languages: []", "types: []"} {
		if strings.Contains(string(raw), unwanted) {
			t.Errorf("migrated config contains %q:\n%s", unwanted, raw)
		}
	}
	// untouched entries keep exactly the fields they had
	if got := dbs["cdn"].Format; got != db.FormatCDNYml {
		t.Errorf("cdn format changed to %q", got)
	}
	if got := dbs["cdn"].File; got != "cdn.yml" {
		t.Errorf("cdn file changed to %q", got)
	}
	if got := len(dbs["cdn"].Languages); got != 0 {
		t.Errorf("cdn got %d injected languages", got)
	}
	if got := len(dbs["cdn"].Types); got != 0 {
		t.Errorf("cdn got %d injected types", got)
	}
	if got := dbs["ip2region"].File; got != "ip2region.xdb" {
		t.Errorf("ip2region file changed to %q", got)
	}
}

// TestMigration2v9WarnsAboutLeftoverFile checks that the database file of the
// removed qqwry entry is reported to the user and left on disk.
func TestMigration2v9WarnsAboutLeftoverFile(t *testing.T) {
	dir := withConfig(t, oldQQWryConfig)

	dataDir := t.TempDir()
	oldDataDir := constant.DataDirPath
	constant.DataDirPath = dataDir
	t.Cleanup(func() { constant.DataDirPath = oldDataDir })

	leftover := filepath.Join(dataDir, "qqwry.dat")
	if err := os.WriteFile(leftover, []byte("old database"), 0644); err != nil {
		t.Fatal(err)
	}

	var logged bytes.Buffer
	oldOutput, oldFlags := log.Writer(), log.Flags()
	log.SetOutput(&logged)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(oldOutput)
		log.SetFlags(oldFlags)
	})

	migration2v9()

	if !strings.Contains(logged.String(), leftover) {
		t.Errorf("migration did not report the leftover file %s:\n%s", leftover, logged.String())
	}
	if _, err := os.Stat(leftover); err != nil {
		t.Errorf("the leftover file must not be deleted: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "config.yaml")); err != nil {
		t.Errorf("config was not written: %v", err)
	}
}

func TestMigration2v9MigratesSelection(t *testing.T) {
	const custom = `databases:
- name: ip2region
  format: ip2region
  file: ip2region.xdb
selected:
  ipv4: chunzhen,ipinfo
  ipv6: zxipv6wry
`
	dir := withConfig(t, custom)

	migration2v9()

	if got := readString(t, dir, "selected.ipv4"); got != "czdb,ipinfo" {
		t.Errorf("selected.ipv4 changed to %q, want %q", got, "czdb,ipinfo")
	}
	if got := readString(t, dir, "selected.ipv6"); got != "zxipv6wry" {
		t.Errorf("selected.ipv6 changed to %q", got)
	}
	if _, found := readDBs(t, dir)["ip2region"]; !found {
		t.Error("ip2region entry lost")
	}
}

// previouslyMigratedConfig is what the first version of the v9 migration left
// behind: the qqwry entry is gone and selected.ipv4 points at czdb, but the
// czdb entry itself was never written to the config.
const previouslyMigratedConfig = `databases:
- name: zxipv6wry
  name-alias: [zxipv6, zx]
  format: zxipv6wry
  file: zxipv6wry.db
- name: ip2region
  format: ip2region
  file: ip2region.xdb
- name: cdn
  format: cdn-yml
  file: cdn.yml
  download-urls:
  - https://example.com/my-cdn.yml
selected:
  ipv4: czdb
  ipv6: zxipv6wry
`

// TestMigration2v9InsertsMissingCZDB covers configs migrated by the earlier v9
// migration: the czdb entry has to show up in the databases list, at the front,
// without disturbing the other entries.
func TestMigration2v9InsertsMissingCZDB(t *testing.T) {
	dir := withConfig(t, previouslyMigratedConfig)
	path := filepath.Join(dir, "config.yaml")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	migration2v9()

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) == string(after) {
		t.Fatal("config was not rewritten although the czdb entry was missing")
	}

	list := readDBList(t, dir)
	if got, want := dbNames(list), []string{"czdb", "zxipv6wry", "ip2region", "cdn"}; !slices.Equal(got, want) {
		t.Fatalf("databases after migration = %v, want %v", got, want)
	}
	checkDefaultCZDBEntry(t, list[0])
	if got := readString(t, dir, "selected.ipv4"); got != "czdb" {
		t.Errorf("selected.ipv4 changed to %q", got)
	}
	if got := readString(t, dir, "selected.ipv6"); got != "zxipv6wry" {
		t.Errorf("selected.ipv6 changed to %q", got)
	}

	// no empty lists are injected into entries that never had them
	for _, unwanted := range []string{"languages: []", "types: []"} {
		if strings.Contains(string(after), unwanted) {
			t.Errorf("migrated config contains %q:\n%s", unwanted, after)
		}
	}

	// the migration is stable: a second run must not rewrite the file again
	migration2v9()
	again, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(again) != string(after) {
		t.Fatalf("second migration rewrote the config:\n%s\n---\n%s", after, again)
	}
}

// TestMigration2v9KeepsExistingCZDBEntry checks that a config already holding a
// czdb entry is left byte for byte untouched.
func TestMigration2v9KeepsExistingCZDBEntry(t *testing.T) {
	const migrated = `databases:
- name: czdb
  name-alias: [chunzhen, cz88]
  format: czdb
  file: /data/my-ipv4.czdb
  zip-entry: cz88_public_v4.czdb
  languages: [zh-CN]
  types: [IPv4]
  download-urls:
  - https://mirror.example.com/czdb?key={key}
- name: ip2region
  format: ip2region
  file: ip2region.xdb
selected:
  ipv4: czdb,ipinfo
  ipv6: zxipv6wry
`
	dir := withConfig(t, migrated)
	before, err := os.ReadFile(filepath.Join(dir, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	migration2v9()

	after, err := os.ReadFile(filepath.Join(dir, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatalf("config rewritten although nothing needed migrating:\n%s", after)
	}
	if got := readDBs(t, dir)["czdb"].File; got != "/data/my-ipv4.czdb" {
		t.Errorf("custom czdb entry overwritten, file = %q", got)
	}
}

// TestMigration2v9KeepsExistingCZDBWhenDroppingQQWry makes sure the qqwry entry
// is dropped without duplicating a czdb entry the user already configured.
func TestMigration2v9KeepsExistingCZDBWhenDroppingQQWry(t *testing.T) {
	const custom = `databases:
- name: qqwry
  format: qqwry
  file: qqwry.dat
- name: czdb
  format: czdb
  file: /data/my-ipv4.czdb
selected:
  ipv4: qqwry
`
	dir := withConfig(t, custom)

	migration2v9()

	list := readDBList(t, dir)
	if got, want := dbNames(list), []string{"czdb"}; !slices.Equal(got, want) {
		t.Fatalf("databases after migration = %v, want %v", got, want)
	}
	if got := list[0].File; got != "/data/my-ipv4.czdb" {
		t.Errorf("custom czdb entry overwritten, file = %q", got)
	}
	if got := readString(t, dir, "selected.ipv4"); got != "czdb" {
		t.Errorf("selected.ipv4 changed to %q", got)
	}
}
