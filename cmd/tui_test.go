package cmd

import (
	"strings"
	"testing"
)

// TestTuiRequiresInteractiveTerminal checks the guard that keeps the TUI out of
// pipes and scripts: RunE must fail with a friendly error instead of starting
// the program. Running under "go test" redirects stdout, which is exactly the
// situation the guard rejects.
func TestTuiRequiresInteractiveTerminal(t *testing.T) {
	if isInteractiveTerminal() {
		t.Skip("stdin and stdout are terminals, the non-interactive guard cannot be exercised")
	}

	err := tuiCmd.RunE(tuiCmd, nil)
	if err == nil {
		t.Fatal("RunE returned no error outside an interactive terminal")
	}
	if !strings.Contains(err.Error(), "交互式终端") {
		t.Errorf("error = %q, want it to mention 交互式终端", err)
	}
}
