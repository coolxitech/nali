package common

import (
	"os"
	"sync"

	"golang.org/x/sys/windows"
)

// utf8CodePage is the UTF-8 code page. golang.org/x/sys/windows does not
// export a CP_UTF8 constant, so the value is kept here.
const utf8CodePage = 65001

// ConsoleUsesGBK reports whether the system ANSI code page is GBK (936) or
// GB18030 (54936), as on Simplified Chinese Windows. Input is then decoded as
// GB18030, which covers both, so the output of legacy tools such as tracert or
// nslookup keeps working when it is piped into nali.
//
// The system ANSI code page is used instead of the console output code page
// because EnableUTF8Console switches the console to UTF-8 at startup, which
// would otherwise disable this automatic GBK/GB18030 decoding.
func ConsoleUsesGBK() bool {
	cp := windows.GetACP()
	return cp == 936 || cp == 54936
}

// EnableUTF8Console switches the Windows console attached to stdout to the
// UTF-8 code page (65001), so Chinese output is not garbled in a console that
// still defaults to code page 936. It does nothing when stdout is not a
// console (a pipe or a file) or when the console already uses UTF-8, in which
// case the returned function is a no-op as well.
//
// The returned function restores the previous code pages. It is never nil and
// is safe to call more than once; it is also reachable through RestoreConsole,
// which Fatal and friends call before exiting.
func EnableUTF8Console() func() {
	restore := enableUTF8Console(windows.Handle(os.Stdout.Fd()))
	restoreConsole = restore
	return restore
}

// enableUTF8Console switches the code pages of the console behind h to UTF-8
// and returns a function restoring the previous ones. Console code pages are a
// property of the console and outlive the process, so the switch is undone by
// the caller as well as by Fatal before os.Exit.
func enableUTF8Console(h windows.Handle) func() {
	noop := func() {}

	// Only touch the code page of an attached console; redirected output must
	// keep the code page untouched.
	var mode uint32
	if err := windows.GetConsoleMode(h, &mode); err != nil {
		return noop
	}

	outCP, err := windows.GetConsoleOutputCP()
	if err != nil || outCP == 0 || outCP == utf8CodePage {
		return noop
	}
	// The input code page is only switched when it could be read, so that
	// every change stays reversible.
	inCP, inErr := windows.GetConsoleCP()

	if err := windows.SetConsoleOutputCP(utf8CodePage); err != nil {
		return noop
	}
	if inErr == nil {
		if err := windows.SetConsoleCP(utf8CodePage); err != nil {
			_ = windows.SetConsoleOutputCP(outCP)
			return noop
		}
	}

	var once sync.Once
	return func() {
		once.Do(func() {
			if outCP != 0 {
				_ = windows.SetConsoleOutputCP(outCP)
			}
			if inErr == nil && inCP != 0 {
				_ = windows.SetConsoleCP(inCP)
			}
		})
	}
}
