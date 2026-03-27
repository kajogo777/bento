package providers

import "context"

// SecretRef identifies a secret and how to resolve it.
type SecretRef struct {
	Source string
	Fields map[string]string
}

// Provider resolves secret references from a specific source.
type Provider interface {
	// Name returns the provider identifier (e.g. "env", "file", "exec").
	Name() string

	// Resolve fetches the secret value described by ref.
	Resolve(ctx context.Context, ref SecretRef) (string, error)

	// Available reports whether this provider can be used in the current environment.
	Available() bool
}
