package secrets

import (
	"context"
	"fmt"

	"github.com/kajogo777/bento/internal/config"
	"github.com/kajogo777/bento/internal/secrets/providers"
)

// HydrateEnv resolves all env entries from the bento.yaml config and returns
// a map of name to resolved value. Literal values are passed through directly.
// Secret references are resolved via their provider. Partial success is
// supported: secrets that fail to resolve are collected as errors while the
// rest proceed.
func HydrateEnv(ctx context.Context, env map[string]config.EnvEntry) (map[string]string, []error) {
	resolved := make(map[string]string, len(env))
	var errs []error

	for name, entry := range env {
		if !entry.IsRef {
			resolved[name] = entry.Value
			continue
		}
		ref := providers.SecretRef{
			Source: entry.Source,
			Fields: entry.Fields,
		}
		val, err := providers.Resolve(ctx, ref)
		if err != nil {
			errs = append(errs, fmt.Errorf("resolving %q: %w", name, err))
			continue
		}
		resolved[name] = val
	}

	return resolved, errs
}
