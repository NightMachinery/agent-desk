// Package emacs holds agent-desk.el; this test runs its ERT suite when an
// Emacs is installed, so `go test ./...` covers it.
package emacs

import (
	"os/exec"
	"testing"
)

func TestERT(t *testing.T) {
	emacs, err := exec.LookPath("emacs")
	if err != nil {
		t.Skip("no emacs on PATH")
	}
	out, err := exec.Command(emacs, "-Q", "--batch", "-L", ".", "-l", "agent-desk-test.el",
		"-f", "ert-run-tests-batch-and-exit").CombinedOutput()
	if err != nil {
		t.Fatalf("ERT failed: %v\n%s", err, out)
	}
}
