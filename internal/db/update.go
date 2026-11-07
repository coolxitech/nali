package db

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/spf13/viper"

	"github.com/coolxitech/nali/pkg/cdn"
	"github.com/coolxitech/nali/pkg/common"
	"github.com/coolxitech/nali/pkg/czdb"
	"github.com/coolxitech/nali/pkg/geoip"
	"github.com/coolxitech/nali/pkg/ip2region"
	"github.com/coolxitech/nali/pkg/zxipv6wry"
)

// UpdateStage is the phase a database update is in.
type UpdateStage string

const (
	StageDownload UpdateStage = "download" // receiving bytes
	StageVerify   UpdateStage = "verify"   // validating the payload
	StageSave     UpdateStage = "save"     // writing the database file
	StageDone     UpdateStage = "done"     // one database finished
	StageFinished UpdateStage = "finished" // the whole run finished (DB == "")
)

// UpdateEvent is one event of a database update.
type UpdateEvent struct {
	DB      string // canonical database name ("" for StageFinished)
	Stage   UpdateStage
	Read    int64  // bytes received so far (StageDownload)
	Total   int64  // announced size, 0 when unknown
	Message string // the exact line the CLI logs for this event
	Err     error  // failure of this database, if any
}

// UpdateDB updates the default or named databases and logs the same lines as
// before.
func UpdateDB(dbNames ...string) {
	UpdateDBs(context.Background(), dbNames, logUpdateEvent)
}

// logUpdateEvent prints an event exactly the way the CLI always has. A message
// may hold several lines, because some failures have always been logged on more
// than one line; each of them is logged separately so every line keeps its own
// log prefix.
func logUpdateEvent(ev UpdateEvent) {
	if ev.Message == "" {
		return
	}
	for _, line := range strings.Split(ev.Message, "\n") {
		log.Println(line)
	}
}

// UpdateDBs updates the named databases (empty means the default list),
// reporting progress through emit and stopping when ctx is done. It returns the
// names of the databases that failed. emit may be nil.
func UpdateDBs(ctx context.Context, names []string, emit func(UpdateEvent)) []string {
	if ctx == nil {
		ctx = context.Background()
	}
	if emit == nil {
		emit = func(UpdateEvent) {}
	}
	if len(names) == 0 {
		names = defaultUpdateList()
	}

	var failed []string
	done := make(map[string]struct{})
	for _, dbName := range names {
		// A cancelled run must not start another database.
		if ctx.Err() != nil {
			break
		}
		plan := updatePlanFor(dbName)
		if _, found := done[plan.key]; !found {
			done[plan.key] = struct{}{}
			if err := plan.run(ctx, emit); err != nil {
				failed = append(failed, plan.name)
			}
		}
	}
	emit(UpdateEvent{Stage: StageFinished})
	return failed
}

// DbNameListForUpdate is always refreshed by a plain `nali update`.
var DbNameListForUpdate = []string{
	"czdb",
	"zxipv6wry",
	"ip2region",
	"cdn",
}

// DbNameListForUpdateIfUsed holds large optional databases. A plain
// `nali update` only refreshes them when they are selected or already
// downloaded; `nali update --db <name>` always updates them.
var DbNameListForUpdateIfUsed = []string{
	"czdb-ipv6",
	"ip2region-ipv6",
	"ipinfo",
}

func defaultUpdateList() []string {
	names := append([]string{}, DbNameListForUpdate...)

	selected := make(map[string]bool)
	for _, key := range []string{"selected.ipv4", "selected.ipv6", "selected.cdn"} {
		for _, name := range strings.Split(viper.GetString(key), ",") {
			if db, found := lookupDb(strings.TrimSpace(name)); found {
				selected[db.Name] = true
			}
		}
	}

	for _, name := range DbNameListForUpdateIfUsed {
		db, found := lookupDb(name)
		if !found {
			continue
		}
		if _, err := os.Stat(db.File); selected[db.Name] || err == nil {
			names = append(names, name)
		}
	}
	return names
}

var DbCheckFunc = map[Format]func([]byte) bool{
	FormatCZDB:      czdb.CheckFile,
	FormatZXIPv6Wry: zxipv6wry.CheckFile,
	FormatMMDB:      geoip.CheckFile,
	FormatIP2Region: ip2region.CheckFile,
	FormatCDNYml:    cdn.CheckFile,
}

// updatePlan is one database update of a run: the key it is deduplicated by, the
// name it is reported under and the work itself, which reports its progress
// through emit.
type updatePlan struct {
	key  string
	name string
	run  func(ctx context.Context, emit func(UpdateEvent)) error
}

// getUpdateFuncByName returns the update of name together with the key used to
// skip a database that was already updated in this run. The update logs the
// lines of the CLI.
func getUpdateFuncByName(name string) (func() error, string) {
	plan := updatePlanFor(name)
	return func() error { return plan.run(context.Background(), logUpdateEvent) }, plan.key
}

// updatePlanFor resolves a database name to the work that updates it. An unknown
// name terminates the process with the message the CLI has always printed.
func updatePlanFor(name string) updatePlan {
	name = strings.TrimSpace(name)
	if db := getDbByName(name); db != nil {
		// CZDB must be handled before the generic download branch: the official
		// endpoint answers with a zip archive holding both the IPv4 and the IPv6
		// database, so saving the response body as-is would store an archive
		// instead of a database.
		if db.Format == FormatCZDB {
			return updatePlan{key: db.Name, name: db.Name, run: func(ctx context.Context, emit func(UpdateEvent)) error {
				emit(downloadStarted(db.Name))
				data, err := czdb.FetchEntryWithContext(ctx, downloadProgress(db.Name, emit), db.ZipEntry, db.DownloadUrls)
				if err == nil {
					emit(verified(db.Name))
				}
				if err == nil && sameContent(db.File, data) {
					emit(upToDate(db.Name, db.File))
					return nil
				}
				if err == nil {
					emit(saving(db.Name))
					err = common.SaveFile(db.File, data)
				}
				if err != nil {
					emit(UpdateEvent{DB: db.Name, Stage: StageDone, Err: err, Message: strings.Join([]string{
						downloadFailed(db.Name, db.File),
						"提示：自动下载需要环境变量 CZDB_DOWNLOAD_KEY；也可在配置文件中配置 download-urls 指向 czdb 文件或压缩包",
						sprintLine("error:", err),
					}, "\n")})
					return err
				}
				emit(downloadSucceeded(db.Name, db.File))
				return nil
			}}
		}

		// direct download if download-url not null
		if len(db.DownloadUrls) > 0 {
			return updatePlan{key: db.Name, name: db.Name, run: func(ctx context.Context, emit func(UpdateEvent)) error {
				emit(downloadStarted(db.Name))
				data, err := common.GetHttpClient().GetWithContext(ctx, downloadProgress(db.Name, emit), db.DownloadUrls...)
				if err == nil {
					emit(verified(db.Name))
				}
				if err == nil && sameContent(db.File, data) {
					emit(upToDate(db.Name, db.File))
					return nil
				}
				if err == nil {
					// validate before saving so a bad download never replaces a working database
					if check, ok := DbCheckFunc[db.Format]; ok && !check(data) {
						err = errors.New("数据库内容出错")
					}
				}
				if err == nil {
					emit(saving(db.Name))
					err = common.SaveFile(db.File, data)
				}
				if err != nil {
					emit(UpdateEvent{DB: db.Name, Stage: StageDone, Err: err, Message: strings.Join([]string{
						downloadFailed(db.Name, db.File),
						sprintLine("下载链接：", db.DownloadUrls),
						sprintLine("error:", err),
					}, "\n")})
					return err
				}
				emit(downloadSucceeded(db.Name, db.File))
				return nil
			}}
		}

		// intenel download func
		switch db.Format {
		case FormatZXIPv6Wry:
			return updatePlan{key: db.Name, name: db.Name, run: func(ctx context.Context, emit func(UpdateEvent)) error {
				emit(UpdateEvent{DB: db.Name, Stage: StageDownload, Message: "正在下载最新 ZX IPv6数据库..."})
				// download and validate without saving, so an unchanged
				// database is not rewritten
				data, err := zxipv6wry.DownloadWithContext(ctx, downloadProgress(db.Name, emit))
				if err == nil {
					emit(verified(db.Name))
				}
				if err == nil && sameContent(db.File, data) {
					emit(upToDate(db.Name, db.File))
					return nil
				}
				if err == nil {
					emit(saving(db.Name))
					err = common.SaveFile(db.File, data)
				}
				if err != nil {
					emit(UpdateEvent{DB: db.Name, Stage: StageDone, Err: err, Message: sprintLine("数据库 ZXIPv6Wry 下载失败:", err)})
					return err
				}
				emit(downloadSucceeded(db.Name, db.File))
				return nil
			}}
		default:
			// The database name deduplicates this branch like the others: a
			// timestamp would collide for two such databases updated in the same
			// clock tick (Windows) and silently skip the second one.
			return updatePlan{key: db.Name, name: db.Name, run: func(ctx context.Context, emit func(UpdateEvent)) error {
				emit(UpdateEvent{DB: db.Name, Stage: StageDone, Message: strings.Join([]string{
					"暂不支持该类型数据库的自动更新",
					"可通过指定数据库的 download-urls 从特定链接下载数据库文件",
				}, "\n")})
				return nil
			}}
		}
	}

	return updatePlan{key: time.Now().String(), name: name, run: func(ctx context.Context, emit func(UpdateEvent)) error {
		common.Fatalln("该名称的数据库未找到：", name)
		return nil
	}}
}

// The helpers below build the messages the CLI logs for one update event, so the
// wording lives next to the update it belongs to. A message never ends with a
// newline: logUpdateEvent logs it line by line. A failure holds the several
// lines the CLI has always printed for it.

// downloadStarted reports that the bytes of dbName are about to be requested.
func downloadStarted(name string) UpdateEvent {
	return UpdateEvent{DB: name, Stage: StageDownload, Message: fmt.Sprintf("正在下载最新 %s 数据库...", name)}
}

// downloadProgress reports the bytes received for name as a download event.
func downloadProgress(name string, emit func(UpdateEvent)) common.DownloadProgress {
	if emit == nil {
		return nil
	}
	return func(read, total int64) {
		emit(UpdateEvent{DB: name, Stage: StageDownload, Read: read, Total: total})
	}
}

// verified reports that the downloaded payload has been validated.
func verified(name string) UpdateEvent {
	return UpdateEvent{DB: name, Stage: StageVerify}
}

// saving reports that the payload is being written to file.
func saving(name string) UpdateEvent {
	return UpdateEvent{DB: name, Stage: StageSave}
}

// upToDate reports that the database file already holds the downloaded content.
func upToDate(name, file string) UpdateEvent {
	return UpdateEvent{DB: name, Stage: StageDone, Message: fmt.Sprintf("%s 数据库已是最新版本: %s", name, file)}
}

// downloadSucceeded reports that the database file has been replaced.
func downloadSucceeded(name, file string) UpdateEvent {
	return UpdateEvent{DB: name, Stage: StageDone, Message: fmt.Sprintf("%s 数据库下载成功: %s", name, file)}
}

// downloadFailed reports that the database file was left untouched. The line has
// always ended with a space before its newline.
func downloadFailed(name, file string) string {
	return fmt.Sprintf("%s 数据库下载失败，请手动下载解压后保存到本地: %s ", name, file)
}

// sprintLine renders one log line with the spacing log.Println would use, but
// without the newline it appends: the caller joins lines instead.
func sprintLine(v ...any) string {
	return strings.TrimSuffix(fmt.Sprintln(v...), "\n")
}

// sameContent reports whether the file at path holds exactly data.
func sameContent(path string, data []byte) bool {
	info, err := os.Stat(path)
	if err != nil || info.Size() != int64(len(data)) {
		return false
	}
	old, err := os.ReadFile(path)
	return err == nil && bytes.Equal(old, data)
}
