package backend

import "fmt"

// allBuiltinBackends returns every built-in backend in priority order.
func allBuiltinBackends() []SecretBackend {
	return []SecretBackend{
		&LocalBackend{},
		&OCIBackend{},
		// Future backends:
		// &VaultBackend{},
		// &OnePasswordBackend{},
		// &AWSParamStoreBackend{},
		// &ExecBackend{},
	}
}

// FindBackend returns the backend registered under the given name, or an error
// if no backend matches. If opts is non-nil and the backend implements
// Configurable, Configure(opts) is called before returning.
func FindBackend(name string, opts map[string]string) (SecretBackend, error) {
	for _, b := range allBuiltinBackends() {
		if b.Name() == name {
			if c, ok := b.(Configurable); ok && opts != nil {
				if err := c.Configure(opts); err != nil {
					return nil, fmt.Errorf("configuring %s backend: %w", name, err)
				}
			}
			return b, nil
		}
	}
	return nil, fmt.Errorf("unknown secrets backend %q; available: %s", name, availableNames())
}

// DefaultBackend returns the local file backend.
func DefaultBackend() SecretBackend {
	return &LocalBackend{}
}

// availableNames returns a comma-separated list of backend names for error messages.
func availableNames() string {
	backends := allBuiltinBackends()
	names := make([]string, len(backends))
	for i, b := range backends {
		names[i] = b.Name()
	}
	result := ""
	for i, n := range names {
		if i > 0 {
			result += ", "
		}
		result += n
	}
	return result
}
