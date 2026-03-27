package secrets

import (
	"context"
	"fmt"

	"github.com/bentoci/bento/internal/config"
	"github.com/bentoci/bento/internal/secrets/providers"
)

// HydrateSecrets resolves all secret references from the bento.yaml config and
// returns a map of secret name to resolved value. Partial success is supported:
// secrets that fail to resolve are collected as errors while the rest proceed.
func HydrateSecrets(ctx context.Context, secrets map[string]config.Secret) (map[string]string, []error) {
	resolved := make(map[string]string, len(secrets))
	var errs []error

	for name, sec := range secrets {
		ref := providers.SecretRef{
			Source: sec.Source,
			Fields: sec.Fields,
		}
		val, err := providers.Resolve(ctx, ref)
		if err != nil {
			errs = append(errs, fmt.Errorf("resolving secret %q: %w", name, err))
			continue
		}
		resolved[name] = val
	}

	return resolved, errs
}
