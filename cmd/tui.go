package cmd

import (
	"context"
	"errors"
	"io"
	"log"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/coolxitech/nali/internal/constant"
	"github.com/coolxitech/nali/internal/db"
	"github.com/coolxitech/nali/internal/tui"
	"github.com/coolxitech/nali/pkg/dbif"
	"github.com/coolxitech/nali/pkg/entity"
)

// tuiCmd represents the tui command
var tuiCmd = &cobra.Command{
	Use:     "tui",
	Short:   "start an interactive terminal UI (TUI)",
	Example: "nali tui",
	Args:    cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Every error from here on is a runtime problem (no terminal, unusable
		// database) that main.go reports, so cobra must neither print the usage
		// text nor the message a second time. A wrong flag or argument fails
		// before this point and still gets the usage hint.
		cmd.SilenceUsage = true
		cmd.SilenceErrors = true

		if !isInteractiveTerminal() {
			return errors.New("tui 需要在交互式终端中运行；在管道或脚本中请直接使用 nali <IP> 或 nali 管道用法")
		}

		databases, err := selectedDatabases()
		if err != nil {
			return err
		}

		model := tui.New(tui.Options{
			Version:   constant.Version,
			Lookup:    lookupLine,
			Databases: databases,
			Update: func(ctx context.Context, emit func(db.UpdateEvent)) {
				// The database packages log to stderr while they download,
				// which tears the alternate screen; keep those lines out of the
				// TUI and restore the previous writer when the run is over.
				writer := log.Writer()
				log.SetOutput(io.Discard)
				defer log.SetOutput(writer)

				db.UpdateDBs(ctx, nil, emit)
			},
			Refresh: func() []tui.DatabaseInfo {
				infos, err := selectedDatabases()
				if err != nil {
					// an invalid selection is reported by the next update; the
					// header simply keeps no status instead of failing here
					return nil
				}
				return infos
			},
			// An opened database keeps its file handle, and Windows refuses to
			// replace a file that is still open, so the TUI calls this before
			// the update writes the new files and again when it is over: the
			// next query then opens the new databases.
			Reset: db.ResetCache,
		})
		_, err = tea.NewProgram(model, tea.WithAltScreen()).Run()
		return err
	},
}

// lookupLine resolves one line against the configured databases. The non-fatal
// lookup keeps the TUI alive when a database cannot be initialised.
func lookupLine(line string) (entity.Entities, error) {
	return entity.ParseLineWith(line, db.FindOrError)
}

// isInteractiveTerminal reports whether both stdin and stdout are terminals.
func isInteractiveTerminal() bool {
	return isTerminal(os.Stdin) && isTerminal(os.Stdout)
}

// isTerminal reports whether f is attached to a terminal, including the Cygwin
// and MSYS pseudo terminals on Windows.
func isTerminal(f *os.File) bool {
	fd := f.Fd()
	return isatty.IsTerminal(fd) || isatty.IsCygwinTerminal(fd)
}

// selection describes one database selection key of the configuration.
type selection struct {
	kind string
	key  string
	typ  dbif.QueryType
}

// selections lists the configuration keys shown in the status bar.
var selections = []selection{
	{kind: "IPv4", key: "selected.ipv4", typ: dbif.TypeIPv4},
	{kind: "IPv6", key: "selected.ipv6", typ: dbif.TypeIPv6},
	{kind: "CDN", key: "selected.cdn", typ: dbif.TypeDomain},
}

// selectedDatabases validates the configured database names and describes them
// for the status bar. An unknown name is reported here instead of letting
// db.GetDBs terminate the process once the TUI is on screen.
func selectedDatabases() ([]tui.DatabaseInfo, error) {
	lang := viper.GetString("selected.lang")
	if lang == "" {
		lang = "zh-CN"
	}

	infos := make([]tui.DatabaseInfo, 0, len(selections))
	for _, sel := range selections {
		names := splitNames(viper.GetString(sel.key))
		if len(names) == 0 {
			// an empty selection falls back to the built-in database
			names = []string{defaultName(sel.typ, lang)}
		}

		info := tui.DatabaseInfo{Kind: sel.kind}
		ready := true
		seen := make(map[string]struct{}, len(names))
		for _, name := range names {
			dbInfo, err := resolveDatabaseName(name)
			if err != nil {
				return nil, err
			}
			if _, found := seen[dbInfo.Name]; found {
				continue
			}
			seen[dbInfo.Name] = struct{}{}
			info.Names = append(info.Names, dbInfo.Name)

			if !fileExists(dbInfo.File) {
				ready = false
			}
		}
		info.Ready = ready
		if !ready {
			info.Note = "文件缺失，请先执行 nali update"
		}
		infos = append(infos, info)
	}
	return infos, nil
}

// splitNames splits a comma separated database selection, dropping blanks.
func splitNames(value string) []string {
	var names []string
	for _, name := range strings.Split(value, ",") {
		if name = strings.TrimSpace(name); name != "" {
			names = append(names, name)
		}
	}
	return names
}

// resolveDatabaseName looks a configured database name or alias up through
// internal/db, so that the TUI reports exactly the messages the CLI reports,
// including the retired qqwry hint.
func resolveDatabaseName(name string) (*db.DB, error) {
	return db.LookupName(name)
}

// defaultName is the built-in database of a query type. It mirrors the fallback
// internal/db uses when a selection is empty, and is only needed for the status
// bar: the actual lookup is resolved by internal/db itself.
func defaultName(typ dbif.QueryType, lang string) string {
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

// fileExists reports whether the database file of a selection is present. The
// files are relative to the data directory, which is the working directory.
func fileExists(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func init() {
	rootCmd.AddCommand(tuiCmd)
}
