package main

import (
	"os"
	"testing"
)

func TestRunReturnsCLIStatus(t *testing.T) {
	originalArgs := os.Args
	t.Cleanup(func() { os.Args = originalArgs })
	os.Args = []string{"agent-plan", "help"}

	if code := run(); code != 0 {
		t.Fatalf("run() code = %d, want 0", code)
	}
}

func TestMainUsesExitFunc(t *testing.T) {
	originalArgs := os.Args
	originalExit := exitFunc
	t.Cleanup(func() {
		os.Args = originalArgs
		exitFunc = originalExit
	})

	os.Args = []string{"agent-plan", "help"}
	got := -1
	exitFunc = func(code int) { got = code }

	main()

	if got != 0 {
		t.Fatalf("exit code = %d, want 0", got)
	}
}
