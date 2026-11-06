//go:build !windows

package common

// ConsoleUsesGBK reports whether the system legacy encoding is GBK (936) or
// GB18030 (54936). Outside Windows, terminals are expected to use UTF-8, so
// this is always false.
func ConsoleUsesGBK() bool {
	return false
}

// EnableUTF8Console is a no-op outside Windows, where terminals are expected
// to use UTF-8 already. The returned function is never nil and only keeps the
// signature of the Windows implementation.
func EnableUTF8Console() func() {
	restore := func() {}
	restoreConsole = restore
	return restore
}
