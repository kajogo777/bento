package secrets

import (
	"context"
	"os"
	"testing"

	"github.com/kajogo777/bento/internal/config"
)

func TestHydrateSecrets_EnvSource(t *testing.T) {
	const envKey = "BENTO_TEST_SECRET_HYDRATE"
	const envVal = "supersecretvalue"

	os.Setenv(envKey, envVal)
	defer os.Unsetenv(envKey)

	secrets := map[string]config.Secret{
		"my_secret": {
			Source: "env",
			Fields: map[string]string{"var": envKey},
		},
	}

	resolved, errs := HydrateSecrets(context.Background(), secrets)
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}
	if resolved["my_secret"] != envVal {
		t.Fatalf("expected %q, got %q", envVal, resolved["my_secret"])
	}
}

func TestHydrateSecrets_MissingEnvVar(t *testing.T) {
	// Make sure the variable is not set.
	os.Unsetenv("BENTO_TEST_MISSING_VAR_XYZ")

	secrets := map[string]config.Secret{
		"missing": {
			Source: "env",
			Fields: map[string]string{"var": "BENTO_TEST_MISSING_VAR_XYZ"},
		},
	}

	resolved, errs := HydrateSecrets(context.Background(), secrets)
	if len(errs) == 0 {
		t.Fatal("expected errors for missing env var")
	}
	if _, ok := resolved["missing"]; ok {
		t.Error("missing secret should not be in resolved map")
	}
}

func TestHydrateSecrets_PartialSuccess(t *testing.T) {
	const envKey = "BENTO_TEST_SECRET_PARTIAL"
	const envVal = "partialvalue"

	os.Setenv(envKey, envVal)
	defer os.Unsetenv(envKey)
	os.Unsetenv("BENTO_TEST_NONEXIST_PARTIAL")

	secrets := map[string]config.Secret{
		"good": {
			Source: "env",
			Fields: map[string]string{"var": envKey},
		},
		"bad": {
			Source: "env",
			Fields: map[string]string{"var": "BENTO_TEST_NONEXIST_PARTIAL"},
		},
	}

	resolved, errs := HydrateSecrets(context.Background(), secrets)
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d", len(errs))
	}
	if resolved["good"] != envVal {
		t.Errorf("expected good=%q, got %q", envVal, resolved["good"])
	}
	if _, ok := resolved["bad"]; ok {
		t.Error("bad secret should not be in resolved map")
	}
}

func TestHydrateSecrets_Empty(t *testing.T) {
	resolved, errs := HydrateSecrets(context.Background(), map[string]config.Secret{})
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}
	if len(resolved) != 0 {
		t.Fatalf("expected empty map, got %v", resolved)
	}
}
