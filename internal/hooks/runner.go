package hooks

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"time"
)

// Runner executes lifecycle hook commands.
type Runner struct {
	workDir string
	timeout time.Duration
}

// NewRunner creates a Runner with the given working directory and timeout in seconds.
func NewRunner(workDir string, timeoutSecs int) *Runner {
	return &Runner{
		workDir: workDir,
		timeout: time.Duration(timeoutSecs) * time.Second,
	}
}

// Run executes the given command string as a lifecycle hook. It uses "sh -c" on
// unix systems and "cmd /c" on Windows. The process is killed if it exceeds the
// configured timeout.
func (r *Runner) Run(hookName, command string) error {
	ctx, cancel := context.WithTimeout(context.Background(), r.timeout)
	defer cancel()

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd", "/c", command)
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", command)
	}

	cmd.Dir = r.workDir

	output, err := cmd.CombinedOutput()
	if len(output) > 0 {
		fmt.Print(string(output))
	}

	if ctx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("hook %q timed out after %s: %s", hookName, r.timeout, command)
	}

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return fmt.Errorf("hook %q failed (exit code %d): %s", hookName, exitErr.ExitCode(), command)
		}
		return fmt.Errorf("hook %q failed: %s: %w", hookName, command, err)
	}

	return nil
}
