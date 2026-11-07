package main

import (
	"log"
	"os"
	"os/signal"

	"github.com/coolxitech/nali/internal/constant"

	"github.com/coolxitech/nali/cmd"
	"github.com/coolxitech/nali/internal/config"
	"github.com/coolxitech/nali/internal/migration"
	"github.com/coolxitech/nali/pkg/common"
)

func main() {
	// On Windows the console is switched to UTF-8 (code page 65001) so that
	// Chinese output is not garbled; the previous code pages are restored on
	// exit. The call is a no-op on other platforms and when output is
	// redirected.
	restore := common.EnableUTF8Console()
	defer restore()

	// Ctrl+C terminates the process without running deferred calls, which would
	// leave the console on code page 65001, so the code pages are restored here
	// as well. The handler is registered after the code pages are switched, so
	// reading the restore function from this goroutine is safe.
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)
	go func() {
		<-interrupt
		common.RestoreConsole()
		os.Exit(130)
	}()

	migration.Run()
	config.ReadConfig(constant.ConfigDirPath)
	if err := cmd.Execute(); err != nil {
		restore() // os.Exit skips deferred calls; restore explicitly first
		log.Fatal(err.Error())
	}
}
