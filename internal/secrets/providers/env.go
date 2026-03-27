package providers

import (
	"context"
	"fmt"
	"os"
)

// EnvProvider resolves secrets from environment variables.
type EnvProvider struct{}

// Name returns "env".
func (p *EnvProvider) Name() string { return "env" }

// Resolve reads the environment variable named by ref.Fields["var"].
func (p *EnvProvider) Resolve(ctx context.Context, ref SecretRef) (string, error) {
	varName := ref.Fields["var"]
	if varName == "" {
		return "", fmt.Errorf("env provider: missing 'var' field")
	}
	val := os.Getenv(varName)
	if val == "" {
		return "", fmt.Errorf("env provider: environment variable %q is empty or not set", varName)
	}
	return val, nil
}

// Available always returns true.
func (p *EnvProvider) Available() bool { return true }
