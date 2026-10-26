package main

import (
	"os"
	"os/exec"
	"testing"
)

func TestMainExec(t *testing.T) {
	if os.Getenv("TEST_MAIN_RUN") == "1" {
		os.Args = []string{"depwhy", "--version"}
		main()
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestMainExec")
	cmd.Env = append(os.Environ(), "TEST_MAIN_RUN=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("cmd failed: %v, out: %s", err, string(out))
	}
}
