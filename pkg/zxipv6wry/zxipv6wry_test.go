package zxipv6wry

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func header(start, counts uint64, size int) []byte {
	data := make([]byte, max(size, 24))
	copy(data, "IPDB")
	data[6], data[7] = 3, 8
	binary.LittleEndian.PutUint64(data[8:], counts)
	binary.LittleEndian.PutUint64(data[16:], start)
	return data[:size]
}

func TestCheckFile(t *testing.T) {
	if !CheckFile(header(24, 2, 24+2*11)) {
		t.Error("valid header rejected")
	}
	for name, data := range map[string][]byte{
		"empty":       nil,
		"html":        []byte("<html>404</html>"),
		"bad magic":   append([]byte("IPDX"), header(24, 2, 46)[4:]...),
		"short":       header(24, 2, 20),
		"truncated":   header(24, 2, 24+2*11-1),
		"zero counts": header(24, 0, 24),
	} {
		if CheckFile(data) {
			t.Errorf("%s accepted", name)
		}
	}
}

// TestNewZXwryRejectsCorruptFile checks that a damaged database file is
// reported to the caller: NewZXwry used to exit the process instead, which
// killed the TUI on its first IPv6 query.
func TestNewZXwryRejectsCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "zxipv6wry.db")
	if err := os.WriteFile(path, []byte("this is not a ZX IPv6 database"), 0644); err != nil {
		t.Fatal(err)
	}

	db, err := NewZXwry(path)
	if err == nil {
		t.Fatalf("expected an error for a corrupt file, got %v", db)
	}
	if db != nil {
		t.Errorf("got a database alongside the error: %v", db)
	}
	for _, want := range []string{"请重新下载", path} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}
