package providers

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// ExecProvider resolves secrets by executing a shell command.
type ExecProvider struct{}

// Name returns "exec".
func (p *ExecProvider) Name() string { return "exec" }

// Resolve runs the command in ref.Fields["command"] via "sh -c" and returns the
// trimmed stdout.
func (p *ExecProvider) Resolve(ctx context.Context, ref SecretRef) (string, error) {
	command := ref.Fields["command"]
	if command == "" {
		return "", fmt.Errorf("exec provider: missing 'command' field")
	}
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("exec provider: running %q: %w", command, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// Available always returns true.
func (p *ExecProvider) Available() bool { return true }
