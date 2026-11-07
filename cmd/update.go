package cmd

import (
	"context"
	"io"
	"log"
	"os"
	"strings"

	"github.com/coolxitech/nali/internal/db"
	"github.com/coolxitech/nali/internal/progress"
	"github.com/coolxitech/nali/internal/repo"

	"github.com/spf13/cobra"
)

// updateCmd represents the update command
var updateCmd = &cobra.Command{
	Use:   "update [--db dbs -v]",
	Short: "update ip databases and cdn, update nali to latest version if -v",
	Long: `update ip databases and cdn. Use commas to separate database names.
Without --db, czdb, zxipv6wry, ip2region and cdn are updated; the larger optional
databases (czdb-ipv6, ip2region-ipv6, ipinfo) are only updated when selected or
already downloaded.
Update nali to latest version if -v`,
	Example: "nali update --db czdb,cdn,ipinfo -v",
	RunE: func(cmd *cobra.Command, args []string) error {
		// Every error from here on is a runtime problem (an unknown database, a
		// failing download) that main.go reports, so cobra must neither print the
		// usage text nor the message a second time. A wrong flag fails before this
		// point and still gets the usage hint.
		cmd.SilenceUsage = true
		cmd.SilenceErrors = true

		DBs, _ := cmd.Flags().GetString("db")

		var DBNameArray []string
		if DBs != "" {
			DBNameArray = strings.Split(DBs, ",")
		}
		// An unknown name is reported here: the update itself terminates the
		// process, and with the errors silenced above that message would be lost.
		for _, name := range DBNameArray {
			if _, err := db.LookupName(name); err != nil {
				return err
			}
		}

		version, _ := cmd.Flags().GetBool("v")
		if version {
			if err := repo.UpdateRepo(); err != nil {
				log.Printf("update nali to latest version failed: %v \n", err)
			}
		}

		if !isTerminal(os.Stderr) {
			// A pipe or a log file cannot be redrawn, so it gets the plain lines
			// the update has always logged.
			db.UpdateDB(DBNameArray...)
			return nil
		}

		// The database packages log to stderr while they download, which would
		// tear the redrawn progress lines; keep those lines out of the output and
		// restore the writer once the summary is printed.
		rep := progress.NewCLI(os.Stderr)
		writer := log.Writer()
		log.SetOutput(io.Discard)
		defer log.SetOutput(writer)

		db.UpdateDBs(context.Background(), DBNameArray, rep.Emit)
		rep.Close()
		return nil
	},
}

func init() {
	updateCmd.PersistentFlags().String("db", "", "choose db you want to update")
	updateCmd.PersistentFlags().BoolP("v", "v", false, "also update nali itself to the latest version")
	rootCmd.AddCommand(updateCmd)
}
