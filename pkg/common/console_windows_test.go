//go:build windows

package common

import (
	"os"
	"testing"

	"golang.org/x/sys/windows"
)

func TestConsoleUsesGBKMatchesACP(t *testing.T) {
	acp := windows.GetACP()
	want := acp == 936 || acp == 54936
	if got := ConsoleUsesGBK(); got != want {
		t.Fatalf("ConsoleUsesGBK() = %v, want %v (system ANSI code page %d)", got, want, acp)
	}
}

func TestEnableUTF8ConsoleSwitchesAndRestores(t *testing.T) {
	// CONOUT$ keeps working when the test output itself is redirected, which
	// is the normal case under `go test`.
	h, err := windows.CreateFile(
		windows.StringToUTF16Ptr("CONOUT$"),
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil,
		windows.OPEN_EXISTING,
		0,
		0,
	)
	if err != nil {
		t.Skipf("no console attached: %v", err)
	}
	defer windows.CloseHandle(h)

	origOut, err := windows.GetConsoleOutputCP()
	if err != nil || origOut == 0 {
		t.Skipf("cannot read the console output code page: %v", err)
	}
	origIn, _ := windows.GetConsoleCP()

	// the code pages belong to the console, not to this process, so they must
	// be back to their original values however the test ends
	defer func() {
		if origOut != 0 {
			_ = windows.SetConsoleOutputCP(origOut)
		}
		if origIn != 0 {
			_ = windows.SetConsoleCP(origIn)
		}
	}()

	restore := enableUTF8Console(h)
	if restore == nil {
		t.Fatal("restore must never be nil")
	}
	defer restore()

	if cp, _ := windows.GetConsoleOutputCP(); cp != utf8CodePage {
		// e.g. a pseudo console that does not implement the switch
		t.Skipf("cannot switch the console code page here (output code page %d)", cp)
	}
	if origIn != 0 {
		// The output code page is what garbles Chinese output; the input code
		// page is only switched when the console honors it (some pseudo
		// consoles do not), so it is checked for reversibility only.
		if cp, err := windows.GetConsoleCP(); err == nil && cp != utf8CodePage {
			t.Logf("this console keeps its input code page at %d", cp)
		}
	}

	restore()
	if cp, err := windows.GetConsoleOutputCP(); err != nil || cp != origOut {
		t.Fatalf("output code page = %d, want %d after restore (%v)", cp, origOut, err)
	}
	if origIn != 0 {
		if cp, err := windows.GetConsoleCP(); err != nil || cp != origIn {
			t.Fatalf("input code page = %d, want %d after restore (%v)", cp, origIn, err)
		}
	}

	// a second call must not change anything
	restore()
	if cp, _ := windows.GetConsoleOutputCP(); cp != origOut {
		t.Fatalf("second restore changed the output code page to %d, want %d", cp, origOut)
	}
}

func TestEnableUTF8ConsoleIsNoopForNonConsole(t *testing.T) {
	// a pipe is not a console: nothing may be switched
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()

	origOut, err := windows.GetConsoleOutputCP()
	if err != nil || origOut == 0 {
		t.Skipf("cannot read the console output code page: %v", err)
	}

	restore := enableUTF8Console(windows.Handle(w.Fd()))
	restore()
	restore()

	if cp, err := windows.GetConsoleOutputCP(); err != nil || cp != origOut {
		t.Fatalf("output code page = %d, want %d (%v)", cp, origOut, err)
	}
}
