package lockfile

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"time"
)

// toolTimeout bounds each external tool run (--go-graph, --cargo-metadata).
// It is a variable so tests can shorten it.
var toolTimeout = 60 * time.Second

// runTool runs name with args in dir, with env added to (and overriding) the
// current environment, and returns its stdout. The error carries the first
// line of the tool's stderr, and says so when the run exceeded toolTimeout.
func runTool(dir string, env []string, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), toolTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...) // later entries win for duplicate keys
	cmd.WaitDelay = time.Second            // don't wait on grandchildren holding the pipes
	out, err := cmd.Output()
	if ctx.Err() != nil {
		return nil, fmt.Errorf("%s timed out after %v", name, toolTimeout)
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		if line, _, _ := bytes.Cut(bytes.TrimSpace(ee.Stderr), []byte("\n")); len(line) > 0 {
			return nil, fmt.Errorf("%w: %s", err, line)
		}
	}
	return out, err
}
