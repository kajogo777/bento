package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	yaml := `store: /tmp/bento-test-store
remote: s3://my-bucket
agent: cursor
task: my-task
env:
  FOO: bar
hooks:
  pre_save: echo pre
  timeout: 60
retention:
  keep_last: 5
  keep_tagged: true
`
	if err := os.WriteFile(filepath.Join(dir, "bento.yaml"), []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if cfg.Store != "/tmp/bento-test-store" {
		t.Errorf("Store = %q, want %q", cfg.Store, "/tmp/bento-test-store")
	}
	if cfg.Remote != "s3://my-bucket" {
		t.Errorf("Remote = %q, want %q", cfg.Remote, "s3://my-bucket")
	}
	if cfg.Agent != "cursor" {
		t.Errorf("Agent = %q, want %q", cfg.Agent, "cursor")
	}
	if cfg.Task != "my-task" {
		t.Errorf("Task = %q, want %q", cfg.Task, "my-task")
	}
	if cfg.Env["FOO"].Value != "bar" {
		t.Errorf("Env[FOO] = %q, want %q", cfg.Env["FOO"].Value, "bar")
	}
	if cfg.Hooks.PreSave != "echo pre" {
		t.Errorf("Hooks.PreSave = %q, want %q", cfg.Hooks.PreSave, "echo pre")
	}
	if cfg.Hooks.Timeout != 60 {
		t.Errorf("Hooks.Timeout = %d, want 60", cfg.Hooks.Timeout)
	}
	if cfg.Retention.KeepLast != 5 {
		t.Errorf("Retention.KeepLast = %d, want 5", cfg.Retention.KeepLast)
	}
	if !cfg.Retention.KeepTagged {
		t.Error("Retention.KeepTagged = false, want true")
	}
}

func TestLoadDefaults(t *testing.T) {
	dir := t.TempDir()
	// Minimal yaml with no store specified.
	yaml := "agent: cursor\n"
	if err := os.WriteFile(filepath.Join(dir, "bento.yaml"), []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	// Store should be the platform default (non-empty).
	if cfg.Store == "" {
		t.Error("Store should default to platform default, got empty string")
	}
	if cfg.Store != DefaultStorePath() {
		t.Errorf("Store = %q, want default %q", cfg.Store, DefaultStorePath())
	}

	// Hooks timeout should be 0 (unset); the runner applies the 300s default.
	if cfg.Hooks.Timeout != 0 {
		t.Errorf("Hooks.Timeout = %d, want 0", cfg.Hooks.Timeout)
	}
}

func TestSaveLoadRoundtrip(t *testing.T) {
	dir := t.TempDir()

	original := &BentoConfig{
		Store:  "/tmp/roundtrip-store",
		Remote: "s3://roundtrip",
		Agent:  "cursor",
		Task:   "roundtrip-task",
		Env:    map[string]EnvEntry{"KEY": NewLiteralEnv("value")},
		Hooks: HooksConfig{
			PreSave: "echo hello",
			Timeout: 120,
		},
		Retention: RetentionConfig{
			KeepLast:   3,
			KeepTagged: true,
		},
	}

	if err := Save(dir, original); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}

	loaded, err := Load(dir)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if loaded.Store != original.Store {
		t.Errorf("Store = %q, want %q", loaded.Store, original.Store)
	}
	if loaded.Remote != original.Remote {
		t.Errorf("Remote = %q, want %q", loaded.Remote, original.Remote)
	}
	if loaded.Agent != original.Agent {
		t.Errorf("Agent = %q, want %q", loaded.Agent, original.Agent)
	}
	if loaded.Task != original.Task {
		t.Errorf("Task = %q, want %q", loaded.Task, original.Task)
	}
	if loaded.Env["KEY"].Value != "value" {
		t.Errorf("Env[KEY] = %q, want %q", loaded.Env["KEY"].Value, "value")
	}
	if loaded.Hooks.PreSave != original.Hooks.PreSave {
		t.Errorf("Hooks.PreSave = %q, want %q", loaded.Hooks.PreSave, original.Hooks.PreSave)
	}
	if loaded.Hooks.Timeout != original.Hooks.Timeout {
		t.Errorf("Hooks.Timeout = %d, want %d", loaded.Hooks.Timeout, original.Hooks.Timeout)
	}
	if loaded.Retention.KeepLast != original.Retention.KeepLast {
		t.Errorf("Retention.KeepLast = %d, want %d", loaded.Retention.KeepLast, original.Retention.KeepLast)
	}
}

func TestLoadMissingFile(t *testing.T) {
	dir := t.TempDir()
	_, err := Load(dir)
	if err == nil {
		t.Fatal("Load should return error for missing bento.yaml")
	}
	if !strings.Contains(err.Error(), "bento.yaml") {
		t.Errorf("error should mention bento.yaml, got: %v", err)
	}
}

func TestExpandPathTilde(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot determine home directory")
	}

	result := expandPath("~/some/path")
	expected := filepath.Join(home, "some/path")
	if result != expected {
		t.Errorf("expandPath(~/some/path) = %q, want %q", result, expected)
	}
}

func TestValidate_Valid(t *testing.T) {
	cases := []struct {
		name string
		yaml string
	}{
		{"no layers", "agent: cursor\n"},
		{"project catch-all by name", `layers:
  - name: project
    patterns: ["**/*.go"]
`},
		{"explicit catch_all", `layers:
  - name: code
    patterns: ["**/*.go"]
  - name: data
    catch_all: true
`},
		{"patterns only", `layers:
  - name: code
    patterns: ["**/*.go"]
  - name: assets
    patterns: ["assets/**"]
`},
		{"layer watch realtime", `layers:
  - name: project
    patterns: ["**"]
    watch: realtime
`},
		{"layer watch periodic", `layers:
  - name: deps
    patterns: ["node_modules/**"]
    watch: periodic
`},
		{"layer watch off", `layers:
  - name: build
    patterns: ["dist/**"]
    watch: off
`},
		{"layer watch omitted", `layers:
  - name: project
    patterns: ["**"]
`},
		{"watch config debounce", "watch:\n  debounce: 5\n"},
		{"watch config message", "watch:\n  message: auto\n"},
		{"watch config skip_secret_scan", "watch:\n  skip_secret_scan: true\n"},
		{"retention keep_last zero", "retention:\n  keep_last: 0\n"},
		{"retention keep_last positive", "retention:\n  keep_last: 50\n"},
		{"hooks timeout zero", "hooks:\n  timeout: 0\n"},
		{"hooks timeout positive", "hooks:\n  timeout: 600\n"},
		{"retention tiers valid", `retention:
  tiers:
    - max_age: 1h
      resolution: 0s
    - max_age: 24h
      resolution: 1h
    - max_age: 168h
      resolution: 24h
`},
		{"retention tier resolution zero", `retention:
  tiers:
    - max_age: 1h
      resolution: 0s
`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "bento.yaml"), []byte(tc.yaml), 0644); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(dir); err != nil {
				t.Errorf("expected no error, got: %v", err)
			}
		})
	}
}

func TestValidate_Errors(t *testing.T) {
	cases := []struct {
		name    string
		yaml    string
		wantErr string
	}{
		{
			"empty layer name",
			`layers:
  - name: ""
    patterns: ["**"]
`,
			"empty name",
		},
		{
			"duplicate layer names",
			`layers:
  - name: code
    patterns: ["**/*.go"]
  - name: code
    catch_all: true
`,
			"duplicate layer name",
		},
		{
			"multiple catch-all layers",
			`layers:
  - name: project
    patterns: ["**"]
  - name: other
    catch_all: true
`,
			"only one catch_all",
		},
		{
			"no patterns non-catch-all",
			`layers:
  - name: code
    patterns: []
`,
			"no patterns",
		},
		{
			"env ref without source",
			`env:
  MY_TOKEN:
    path: /secret/token
`,
			"no 'source' field",
		},
		{
			"invalid layer watch value",
			`layers:
  - name: project
    patterns: ["**"]
    watch: instant
`,
			`invalid watch value "instant"`,
		},
		{
			"layer watch typo",
			`layers:
  - name: deps
    patterns: ["node_modules/**"]
    watch: polll
`,
			`invalid watch value "polll"`,
		},
		{
			"watch debounce negative",
			"watch:\n  debounce: -1\n",
			"watch.debounce must be >= 1",
		},
		{
			"retention keep_last negative",
			"retention:\n  keep_last: -5\n",
			"retention.keep_last must be >= 0",
		},
		{
			"hooks timeout negative",
			"hooks:\n  timeout: -10\n",
			"hooks.timeout must be >= 0",
		},
		{
			"retention tier zero max_age",
			`retention:
  tiers:
    - max_age: 0s
      resolution: 0s
`,
			"max_age must be a positive duration",
		},
		{
			"retention tier resolution exceeds max_age",
			`retention:
  tiers:
    - max_age: 1h
      resolution: 2h
`,
			"resolution (2h0m0s) must be smaller than max_age (1h0m0s)",
		},
		{
			"retention tiers not ascending",
			`retention:
  tiers:
    - max_age: 24h
      resolution: 1h
    - max_age: 1h
      resolution: 0s
`,
			"max_age (1h0m0s) must be larger than previous tier (24h0m0s)",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "bento.yaml"), []byte(tc.yaml), 0644); err != nil {
				t.Fatal(err)
			}
			_, err := Load(dir)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error %q should contain %q", err.Error(), tc.wantErr)
			}
		})
	}
}

func TestExpandPathNoTilde(t *testing.T) {
	result := expandPath("/absolute/path")
	if result != "/absolute/path" {
		t.Errorf("expandPath(/absolute/path) = %q, want %q", result, "/absolute/path")
	}
}

func TestExpandPathEnvVar(t *testing.T) {
	t.Setenv("BENTO_TEST_VAR", "expanded")
	result := expandPath("/path/$BENTO_TEST_VAR/sub")
	if result != "/path/expanded/sub" {
		t.Errorf("expandPath with env var = %q, want %q", result, "/path/expanded/sub")
	}
}
