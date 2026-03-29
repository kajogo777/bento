package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kajogo777/bento/internal/config"
)

// execEnv runs "bento env <args>" against a workspace dir, capturing all stdout.
func execEnv(t *testing.T, dir string, args ...string) string {
	t.Helper()

	// Capture stdout since commands use fmt.Printf (not cmd.OutOrStdout).
	oldStdout := os.Stdout
	r, w, pipeErr := os.Pipe()
	if pipeErr != nil {
		t.Fatalf("os.Pipe: %v", pipeErr)
	}
	os.Stdout = w

	rootCmd := NewRootCmd("test")
	fullArgs := append([]string{"env", "--dir", dir}, args...)
	rootCmd.SetArgs(fullArgs)
	err := rootCmd.Execute()

	w.Close() //nolint:errcheck
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	out := buf.String()

	if err != nil {
		t.Fatalf("bento env %s failed: %v\noutput: %s", strings.Join(args, " "), err, out)
	}
	return out
}

// execEnvExpectErr runs "bento env <args>" and expects a non-zero exit.
func execEnvExpectErr(t *testing.T, dir string, args ...string) string {
	t.Helper()

	oldStdout := os.Stdout
	r, w, pipeErr := os.Pipe()
	if pipeErr != nil {
		t.Fatalf("os.Pipe: %v", pipeErr)
	}
	os.Stdout = w

	rootCmd := NewRootCmd("test")
	fullArgs := append([]string{"env", "--dir", dir}, args...)
	rootCmd.SetArgs(fullArgs)
	err := rootCmd.Execute()

	w.Close() //nolint:errcheck
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	out := buf.String()

	if err == nil {
		t.Fatalf("bento env %s expected error but succeeded\noutput: %s", strings.Join(args, " "), out)
	}
	return out
}

// setupWorkspace creates a temp dir with a minimal bento.yaml.
func setupWorkspace(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	storeDir := t.TempDir()
	yaml := "id: ws-test\nstore: " + storeDir + "\n"
	if err := os.WriteFile(filepath.Join(dir, "bento.yaml"), []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// loadEnv is a helper that loads the config and returns the env map.
func loadEnv(t *testing.T, dir string) map[string]config.EnvEntry {
	t.Helper()
	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatalf("loading config: %v", err)
	}
	return cfg.Env
}

// ---------------------------------------------------------------------------
// set: plain env var
// ---------------------------------------------------------------------------

func TestEnvSet_Literal(t *testing.T) {
	dir := setupWorkspace(t)

	execEnv(t, dir, "set", "NODE_ENV", "development")

	env := loadEnv(t, dir)
	entry, ok := env["NODE_ENV"]
	if !ok {
		t.Fatal("NODE_ENV not found in env")
	}
	if entry.IsRef {
		t.Error("NODE_ENV should be a literal, got ref")
	}
	if entry.Value != "development" {
		t.Errorf("NODE_ENV = %q, want %q", entry.Value, "development")
	}
}

// ---------------------------------------------------------------------------
// set: secret reference
// ---------------------------------------------------------------------------

func TestEnvSet_SecretRef(t *testing.T) {
	dir := setupWorkspace(t)

	execEnv(t, dir, "set", "DATABASE_URL", "--source", "env", "--var", "DATABASE_URL")

	env := loadEnv(t, dir)
	entry, ok := env["DATABASE_URL"]
	if !ok {
		t.Fatal("DATABASE_URL not found in env")
	}
	if !entry.IsRef {
		t.Fatal("DATABASE_URL should be a ref, got literal")
	}
	if entry.Source != "env" {
		t.Errorf("Source = %q, want %q", entry.Source, "env")
	}
	if entry.Fields["var"] != "DATABASE_URL" {
		t.Errorf("Fields[var] = %q, want %q", entry.Fields["var"], "DATABASE_URL")
	}
}

func TestEnvSet_SecretRef_File(t *testing.T) {
	dir := setupWorkspace(t)

	execEnv(t, dir, "set", "API_KEY", "--source", "file", "--path", "/run/secrets/api-key")

	env := loadEnv(t, dir)
	entry := env["API_KEY"]
	if !entry.IsRef {
		t.Fatal("API_KEY should be a ref")
	}
	if entry.Source != "file" {
		t.Errorf("Source = %q, want %q", entry.Source, "file")
	}
	if entry.Fields["path"] != "/run/secrets/api-key" {
		t.Errorf("Fields[path] = %q, want %q", entry.Fields["path"], "/run/secrets/api-key")
	}
}

func TestEnvSet_SecretRef_Exec(t *testing.T) {
	dir := setupWorkspace(t)

	execEnv(t, dir, "set", "TOKEN", "--source", "exec", "--command", "vault read secret/token")

	env := loadEnv(t, dir)
	entry := env["TOKEN"]
	if !entry.IsRef || entry.Source != "exec" {
		t.Fatalf("expected exec ref, got isRef=%v source=%q", entry.IsRef, entry.Source)
	}
	if entry.Fields["command"] != "vault read secret/token" {
		t.Errorf("Fields[command] = %q, want %q", entry.Fields["command"], "vault read secret/token")
	}
}

// ---------------------------------------------------------------------------
// set: overwrite literal → secret and vice-versa
// ---------------------------------------------------------------------------

func TestEnvSet_LiteralOverwritesRef(t *testing.T) {
	dir := setupWorkspace(t)

	execEnv(t, dir, "set", "DB", "--source", "env", "--var", "DB_URL")
	execEnv(t, dir, "set", "DB", "postgres://localhost/mydb")

	env := loadEnv(t, dir)
	entry := env["DB"]
	if entry.IsRef {
		t.Error("DB should be a literal after overwrite, got ref")
	}
	if entry.Value != "postgres://localhost/mydb" {
		t.Errorf("DB = %q, want %q", entry.Value, "postgres://localhost/mydb")
	}
}

func TestEnvSet_RefOverwritesLiteral(t *testing.T) {
	dir := setupWorkspace(t)

	execEnv(t, dir, "set", "DB", "postgres://localhost/mydb")
	execEnv(t, dir, "set", "DB", "--source", "env", "--var", "DB_URL")

	env := loadEnv(t, dir)
	entry := env["DB"]
	if !entry.IsRef {
		t.Error("DB should be a ref after overwrite, got literal")
	}
	if entry.Source != "env" {
		t.Errorf("Source = %q, want %q", entry.Source, "env")
	}
}

// ---------------------------------------------------------------------------
// set: multiple values persist correctly
// ---------------------------------------------------------------------------

func TestEnvSet_Multiple(t *testing.T) {
	dir := setupWorkspace(t)

	execEnv(t, dir, "set", "A", "1")
	execEnv(t, dir, "set", "B", "2")
	execEnv(t, dir, "set", "C", "--source", "env", "--var", "SECRET_C")

	env := loadEnv(t, dir)
	if len(env) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(env))
	}
	if env["A"].Value != "1" {
		t.Errorf("A = %q, want %q", env["A"].Value, "1")
	}
	if env["B"].Value != "2" {
		t.Errorf("B = %q, want %q", env["B"].Value, "2")
	}
	if !env["C"].IsRef || env["C"].Source != "env" {
		t.Errorf("C: unexpected state isRef=%v source=%q", env["C"].IsRef, env["C"].Source)
	}
}

// ---------------------------------------------------------------------------
// set: validation errors
// ---------------------------------------------------------------------------

func TestEnvSet_MissingValue(t *testing.T) {
	dir := setupWorkspace(t)
	execEnvExpectErr(t, dir, "set", "ALONE")
}

func TestEnvSet_SourceWithPositionalValue(t *testing.T) {
	dir := setupWorkspace(t)
	execEnvExpectErr(t, dir, "set", "KEY", "val", "--source", "env", "--var", "X")
}

func TestEnvSet_SourceWithoutFields(t *testing.T) {
	dir := setupWorkspace(t)
	execEnvExpectErr(t, dir, "set", "KEY", "--source", "env")
}

func TestEnvSet_NoBentoYaml(t *testing.T) {
	dir := t.TempDir() // no bento.yaml
	execEnvExpectErr(t, dir, "set", "KEY", "val")
}

// ---------------------------------------------------------------------------
// unset
// ---------------------------------------------------------------------------

func TestEnvUnset_Literal(t *testing.T) {
	dir := setupWorkspace(t)
	execEnv(t, dir, "set", "FOO", "bar")
	execEnv(t, dir, "unset", "FOO")

	env := loadEnv(t, dir)
	if _, ok := env["FOO"]; ok {
		t.Error("FOO should be removed")
	}
}

func TestEnvUnset_Secret(t *testing.T) {
	dir := setupWorkspace(t)
	execEnv(t, dir, "set", "SECRET", "--source", "env", "--var", "S")
	execEnv(t, dir, "unset", "SECRET")

	env := loadEnv(t, dir)
	if _, ok := env["SECRET"]; ok {
		t.Error("SECRET should be removed")
	}
}

func TestEnvUnset_PreservesOthers(t *testing.T) {
	dir := setupWorkspace(t)
	execEnv(t, dir, "set", "KEEP", "yes")
	execEnv(t, dir, "set", "DROP", "no")
	execEnv(t, dir, "unset", "DROP")

	env := loadEnv(t, dir)
	if _, ok := env["DROP"]; ok {
		t.Error("DROP should be removed")
	}
	if env["KEEP"].Value != "yes" {
		t.Errorf("KEEP = %q, want %q", env["KEEP"].Value, "yes")
	}
}

func TestEnvUnset_NotFound(t *testing.T) {
	dir := setupWorkspace(t)
	execEnvExpectErr(t, dir, "unset", "NOPE")
}

// ---------------------------------------------------------------------------
// show
// ---------------------------------------------------------------------------

func TestEnvShow_Empty(t *testing.T) {
	dir := setupWorkspace(t)
	out := execEnv(t, dir, "show")
	if !strings.Contains(out, "No environment variables configured") {
		t.Errorf("expected empty message, got:\n%s", out)
	}
}

func TestEnvShow_Mixed(t *testing.T) {
	dir := setupWorkspace(t)
	execEnv(t, dir, "set", "NODE_ENV", "production")
	execEnv(t, dir, "set", "DB", "--source", "env", "--var", "DATABASE_URL")

	out := execEnv(t, dir, "show")
	if !strings.Contains(out, "NODE_ENV=production") {
		t.Errorf("show should display NODE_ENV=production, got:\n%s", out)
	}
	if !strings.Contains(out, "DB") && !strings.Contains(out, "env") {
		t.Errorf("show should display DB secret ref, got:\n%s", out)
	}
}

func TestEnvShow_Resolve(t *testing.T) {
	dir := setupWorkspace(t)

	t.Setenv("BENTO_TEST_RESOLVE_VAR", "resolved-value")
	execEnv(t, dir, "set", "MY_VAR", "--source", "env", "--var", "BENTO_TEST_RESOLVE_VAR")

	out := execEnv(t, dir, "show", "--resolve")

	// Should show masked value (r***d pattern for "resolved-value").
	if !strings.Contains(out, "****") && !strings.Contains(out, "r*") {
		t.Errorf("show --resolve should display masked value, got:\n%s", out)
	}
}

func TestEnvShow_Reveal(t *testing.T) {
	dir := setupWorkspace(t)

	t.Setenv("BENTO_TEST_REVEAL_VAR", "top-secret")
	execEnv(t, dir, "set", "S", "--source", "env", "--var", "BENTO_TEST_REVEAL_VAR")

	out := execEnv(t, dir, "show", "--reveal")
	if !strings.Contains(out, "top-secret") {
		t.Errorf("show --reveal should display cleartext, got:\n%s", out)
	}
}

func TestEnvShow_ResolveFailed(t *testing.T) {
	dir := setupWorkspace(t)

	_ = os.Unsetenv("BENTO_NONEXISTENT_99")
	execEnv(t, dir, "set", "BAD", "--source", "env", "--var", "BENTO_NONEXISTENT_99")

	out := execEnv(t, dir, "show", "--resolve")
	if !strings.Contains(out, "failed to resolve") {
		t.Errorf("show --resolve should indicate failure, got:\n%s", out)
	}
}

// ---------------------------------------------------------------------------
// export
// ---------------------------------------------------------------------------

func TestEnvExport_Stdout(t *testing.T) {
	dir := setupWorkspace(t)

	t.Setenv("BENTO_TEST_EXPORT_VAR", "secret123")
	execEnv(t, dir, "set", "PLAIN", "hello")
	execEnv(t, dir, "set", "SECRET", "--source", "env", "--var", "BENTO_TEST_EXPORT_VAR")

	out := execEnv(t, dir, "export")
	if !strings.Contains(out, "PLAIN=hello") {
		t.Errorf("export should contain PLAIN=hello, got:\n%s", out)
	}
	if !strings.Contains(out, "SECRET=secret123") {
		t.Errorf("export should contain resolved SECRET=secret123, got:\n%s", out)
	}
}

func TestEnvExport_Sorted(t *testing.T) {
	dir := setupWorkspace(t)
	execEnv(t, dir, "set", "ZZZ", "last")
	execEnv(t, dir, "set", "AAA", "first")

	out := execEnv(t, dir, "export")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected 2 lines, got %d: %q", len(lines), out)
	}
	if !strings.HasPrefix(lines[0], "AAA=") {
		t.Errorf("first line should be AAA=, got %q", lines[0])
	}
	if !strings.HasPrefix(lines[1], "ZZZ=") {
		t.Errorf("second line should be ZZZ=, got %q", lines[1])
	}
}

func TestEnvExport_ToFile(t *testing.T) {
	dir := setupWorkspace(t)
	execEnv(t, dir, "set", "KEY", "value")

	execEnv(t, dir, "export", "-o", ".env")

	data, err := os.ReadFile(filepath.Join(dir, ".env"))
	if err != nil {
		t.Fatalf("reading .env: %v", err)
	}
	if !strings.Contains(string(data), "KEY=value") {
		t.Errorf(".env should contain KEY=value, got: %s", data)
	}

	// Check 0600 permissions.
	info, _ := os.Stat(filepath.Join(dir, ".env"))
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf(".env permissions = %o, want 0600", perm)
	}
}

func TestEnvExport_WithTemplate(t *testing.T) {
	dir := setupWorkspace(t)

	// Write template.
	tmpl := "# DB config\nDB_HOST=placeholder\nDB_PORT=5432\n"
	if err := os.WriteFile(filepath.Join(dir, ".env.example"), []byte(tmpl), 0644); err != nil {
		t.Fatal(err)
	}

	execEnv(t, dir, "set", "DB_HOST", "localhost")

	execEnv(t, dir, "export", "-o", ".env", "--template", ".env.example")

	data, err := os.ReadFile(filepath.Join(dir, ".env"))
	if err != nil {
		t.Fatalf("reading .env: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "DB_HOST=localhost") {
		t.Errorf(".env should have DB_HOST=localhost, got:\n%s", content)
	}
	if !strings.Contains(content, "DB_PORT=5432") {
		t.Errorf(".env should preserve DB_PORT=5432, got:\n%s", content)
	}
	if !strings.Contains(content, "# DB config") {
		t.Errorf(".env should preserve comments, got:\n%s", content)
	}
}

func TestEnvExport_Empty(t *testing.T) {
	dir := setupWorkspace(t)
	// Should not error, just print nothing useful.
	execEnv(t, dir, "export")
}

func TestEnvExport_PathTraversal(t *testing.T) {
	dir := setupWorkspace(t)
	execEnv(t, dir, "set", "K", "v")

	execEnvExpectErr(t, dir, "export", "-o", "../../../tmp/evil")
	execEnvExpectErr(t, dir, "export", "-o", ".env", "--template", "../../../etc/passwd")
}

// ---------------------------------------------------------------------------
// YAML roundtrip: mixed literals and refs survive save/load
// ---------------------------------------------------------------------------

func TestEnvYAMLRoundtrip(t *testing.T) {
	dir := setupWorkspace(t)

	execEnv(t, dir, "set", "PLAIN", "hello")
	execEnv(t, dir, "set", "REF", "--source", "file", "--path", "/tmp/secret")

	// Load and verify the config survived roundtrip.
	env := loadEnv(t, dir)

	plain := env["PLAIN"]
	if plain.IsRef || plain.Value != "hello" {
		t.Errorf("PLAIN: isRef=%v value=%q, want literal 'hello'", plain.IsRef, plain.Value)
	}

	ref := env["REF"]
	if !ref.IsRef || ref.Source != "file" || ref.Fields["path"] != "/tmp/secret" {
		t.Errorf("REF: isRef=%v source=%q path=%q, want file ref", ref.IsRef, ref.Source, ref.Fields["path"])
	}

	// Verify the YAML on disk has the right shape.
	data, _ := os.ReadFile(filepath.Join(dir, "bento.yaml"))
	content := string(data)
	if !strings.Contains(content, "PLAIN: hello") {
		t.Errorf("YAML should have scalar 'PLAIN: hello', got:\n%s", content)
	}
	if !strings.Contains(content, "source: file") {
		t.Errorf("YAML should have 'source: file' for REF, got:\n%s", content)
	}
}
