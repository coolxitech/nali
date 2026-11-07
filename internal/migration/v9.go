package migration

import (
	"log"
	"path/filepath"
	"strings"

	"github.com/coolxitech/nali/internal/constant"
	"github.com/coolxitech/nali/internal/db"
	"github.com/coolxitech/nali/pkg/common"
	"github.com/spf13/viper"
)

const (
	// oldChunzhenName is both the name and the format of the retired qqwry
	// database; oldChunzhenAlias is the alias it could also be selected by.
	oldChunzhenName  = "qqwry"
	oldChunzhenAlias = "chunzhen"
	// newChunzhenName is the name of the CZDB database replacing qqwry.
	newChunzhenName = "czdb"
	// oldChunzhenFile is the file name of the retired qqwry database. It is
	// left on disk after the migration (nali never deletes user files), so the
	// user is told it can be removed.
	oldChunzhenFile = "qqwry.dat"
)

// migration2v9 drops the retired Chunzhen qqwry database from the config,
// writes the built-in czdb definition into the databases list in its place and
// points selected.ipv4 at czdb.
func migration2v9() {
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(constant.ConfigDirPath)

	err := viper.ReadInConfig()
	if err != nil {
		return
	}

	dbList := db.List{}
	err = viper.UnmarshalKey("databases", &dbList)
	if err != nil {
		common.Fatalln("Config invalid:", err)
	}

	changed := false

	migratedList, removed, listChanged := migrateChunzhenDBList(dbList)
	if listChanged {
		changed = true
		viper.Set("databases", migratedList)
	}

	if selected, ok := migrateChunzhenSelection(viper.GetString("selected.ipv4")); ok {
		changed = true
		viper.Set("selected.ipv4", selected)
	}

	if !changed {
		return
	}

	if err = viper.WriteConfig(); err != nil {
		log.Println(err)
		return
	}
	if removed {
		log.Println("已将旧纯真 qqwry 库替换为 czdb，并把 czdb 默认条目写入配置；首次使用需设置 CZDB_KEY 环境变量")
		return
	}
	log.Println("已把 czdb 默认条目写入配置；首次使用需设置 CZDB_KEY 环境变量")
}

// czdbDefault clones the built-in czdb definition. A clone is returned so the
// migration never shares (and therefore never mutates) the package level
// defaults, which the rest of nali reads directly.
func czdbDefault() *db.DB {
	for _, d := range db.GetDefaultDBList() {
		if d.Name != newChunzhenName {
			continue
		}
		cloned := *d
		cloned.NameAlias = append([]string(nil), d.NameAlias...)
		cloned.Languages = append([]string(nil), d.Languages...)
		cloned.Types = append([]db.Type(nil), d.Types...)
		cloned.DownloadUrls = append([]string(nil), d.DownloadUrls...)
		return &cloned
	}
	return nil
}

// migrateChunzhenDBList removes every entry describing the retired qqwry
// database and writes the built-in czdb definition in place of the first one.
// A config that has already lost the qqwry entry but still has no czdb entry
// (migrated by an earlier version) gets the czdb entry prepended, so the
// default IPv4 database is visible in the config. An existing czdb entry is
// never touched, and no second one is added.
func migrateChunzhenDBList(dbList db.List) (db.List, bool, bool) {
	hasCZDB := false
	for _, adb := range dbList {
		if adb.Name == newChunzhenName {
			hasCZDB = true
			break
		}
	}

	kept := make(db.List, 0, len(dbList)+1)
	removed := false
	replaced := false
	for _, adb := range dbList {
		if !isOldChunzhen(adb) {
			kept = append(kept, adb)
			continue
		}
		removed = true
		warnLeftoverQQWryFile(adb)
		if hasCZDB || replaced {
			// The czdb entry (user defined or the one just written) already
			// covers this database, only the qqwry entry has to go.
			continue
		}
		if czdb := czdbDefault(); czdb != nil {
			kept = append(kept, czdb)
			replaced = true
			hasCZDB = true
		}
	}

	if hasCZDB {
		return kept, removed, removed
	}
	if czdb := czdbDefault(); czdb != nil {
		return append(db.List{czdb}, kept...), removed, true
	}
	return kept, removed, removed
}

// isOldChunzhen reports whether adb describes the retired qqwry database.
func isOldChunzhen(adb *db.DB) bool {
	return adb.Name == oldChunzhenName || string(adb.Format) == oldChunzhenName
}

// warnLeftoverQQWryFile tells the user that the removed entry's database file
// is still on disk. The file is deliberately not deleted: it belongs to the
// user and may be referenced from elsewhere.
func warnLeftoverQQWryFile(adb *db.DB) {
	base := filepath.Base(adb.File)
	if adb.File == "" || base != oldChunzhenFile {
		return
	}
	log.Printf("旧纯真数据库文件 %s 已不再使用，可手动删除\n", filepath.Join(constant.DataDirPath, base))
}

// migrateChunzhenSelection replaces the retired qqwry names in a comma
// separated database selection with czdb, keeping the remaining names in
// order and without duplicates. It reports whether anything changed.
func migrateChunzhenSelection(selected string) (string, bool) {
	names := make([]string, 0, 4)
	for _, name := range strings.Split(selected, ",") {
		if name = strings.TrimSpace(name); name != "" {
			names = append(names, name)
		}
	}

	migrated := false
	for i, name := range names {
		if name == oldChunzhenName || name == oldChunzhenAlias {
			names[i] = newChunzhenName
			migrated = true
		}
	}
	if !migrated {
		return "", false
	}

	result := make([]string, 0, len(names))
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		if _, found := seen[name]; found {
			continue
		}
		seen[name] = struct{}{}
		result = append(result, name)
	}
	return strings.Join(result, ","), true
}
