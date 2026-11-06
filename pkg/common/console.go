package common

import "log"

// restoreConsole holds the restore function registered by EnableUTF8Console.
var restoreConsole func()

// RestoreConsole restores the console code pages saved by EnableUTF8Console.
// It is safe to call at any time, including before EnableUTF8Console (no-op)
// and more than once.
func RestoreConsole() {
	if restoreConsole != nil {
		restoreConsole()
	}
}

// Fatal is log.Fatal after restoring the console code pages. Windows console
// code pages are shared by every process attached to the console and survive
// the process, so they must be restored before os.Exit skips deferred calls.
func Fatal(v ...any) {
	RestoreConsole()
	log.Fatal(v...)
}

// Fatalf is log.Fatalf after restoring the console code pages.
func Fatalf(format string, v ...any) {
	RestoreConsole()
	log.Fatalf(format, v...)
}

// Fatalln is log.Fatalln after restoring the console code pages.
func Fatalln(v ...any) {
	RestoreConsole()
	log.Fatalln(v...)
}
