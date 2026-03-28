package secrets

import (
	"context"
	"os"
	"testing"

	"github.com/kajogo777/bento/internal/config"
)

func TestHydrateEnv_LiteralValues(t *testing.T) {
	env := map[string]config.EnvEntry{
		"NODE_ENV": config.NewLiteralEnv("development"),
		"PORT":     config.NewLiteralEnv("3000"),
	}

	resolved, errs := HydrateEnv(context.Background(), env)
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}
	if resolved["NODE_ENV"] != "development" {
		t.Errorf("NODE_ENV = %q, want %q", resolved["NODE_ENV"], "development")
	}
	if resolved["PORT"] != "3000" {
		t.Errorf("PORT = %q, want %q", resolved["PORT"], "3000")
	}
}

func TestHydrateEnv_SecretRef(t *testing.T) {
	const envKey = "BENTO_TEST_SECRET_HYDRATE"
	const envVal = "supersecretvalue"

	t.Setenv(envKey, envVal)

	env := map[string]config.EnvEntry{
		"my_secret": config.NewSecretEnv("env", map[string]string{"var": envKey}),
	}

	resolved, errs := HydrateEnv(context.Background(), env)
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}
	if resolved["my_secret"] != envVal {
		t.Fatalf("expected %q, got %q", envVal, resolved["my_secret"])
	}
}

func TestHydrateEnv_MissingEnvVar(t *testing.T) {
	// Make sure the variable is not set.
	_ = os.Unsetenv("BENTO_TEST_MISSING_VAR_XYZ")

	env := map[string]config.EnvEntry{
		"missing": config.NewSecretEnv("env", map[string]string{"var": "BENTO_TEST_MISSING_VAR_XYZ"}),
	}

	resolved, errs := HydrateEnv(context.Background(), env)
	if len(errs) == 0 {
		t.Fatal("expected errors for missing env var")
	}
	if _, ok := resolved["missing"]; ok {
		t.Error("missing secret should not be in resolved map")
	}
}

func TestHydrateEnv_PartialSuccess(t *testing.T) {
	const envKey = "BENTO_TEST_SECRET_PARTIAL"
	const envVal = "partialvalue"

	t.Setenv(envKey, envVal)
	_ = os.Unsetenv("BENTO_TEST_NONEXIST_PARTIAL")

	env := map[string]config.EnvEntry{
		"good":    config.NewSecretEnv("env", map[string]string{"var": envKey}),
		"bad":     config.NewSecretEnv("env", map[string]string{"var": "BENTO_TEST_NONEXIST_PARTIAL"}),
		"literal": config.NewLiteralEnv("hello"),
	}

	resolved, errs := HydrateEnv(context.Background(), env)
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d", len(errs))
	}
	if resolved["good"] != envVal {
		t.Errorf("expected good=%q, got %q", envVal, resolved["good"])
	}
	if _, ok := resolved["bad"]; ok {
		t.Error("bad secret should not be in resolved map")
	}
	if resolved["literal"] != "hello" {
		t.Errorf("expected literal=%q, got %q", "hello", resolved["literal"])
	}
}

func TestHydrateEnv_Empty(t *testing.T) {
	resolved, errs := HydrateEnv(context.Background(), map[string]config.EnvEntry{})
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}
	if len(resolved) != 0 {
		t.Fatalf("expected empty map, got %v", resolved)
	}
}
