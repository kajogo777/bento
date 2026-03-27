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
sync: auto
harness: cursor
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
	if cfg.Sync != "auto" {
		t.Errorf("Sync = %q, want %q", cfg.Sync, "auto")
	}
	if cfg.Harness != "cursor" {
		t.Errorf("Harness = %q, want %q", cfg.Harness, "cursor")
	}
	if cfg.Task != "my-task" {
		t.Errorf("Task = %q, want %q", cfg.Task, "my-task")
	}
	if cfg.Env["FOO"] != "bar" {
		t.Errorf("Env[FOO] = %q, want %q", cfg.Env["FOO"], "bar")
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
	// Minimal yaml with no store or sync specified.
	yaml := "harness: cursor\n"
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

	// Sync should default to "manual".
	if cfg.Sync != "manual" {
		t.Errorf("Sync = %q, want %q", cfg.Sync, "manual")
	}

	// Hooks timeout should default to 300.
	if cfg.Hooks.Timeout != 300 {
		t.Errorf("Hooks.Timeout = %d, want 300", cfg.Hooks.Timeout)
	}
}

func TestSaveLoadRoundtrip(t *testing.T) {
	dir := t.TempDir()

	original := &BentoConfig{
		Store:   "/tmp/roundtrip-store",
		Remote:  "s3://roundtrip",
		Sync:    "auto",
		Harness: "cursor",
		Task:    "roundtrip-task",
		Env:     map[string]string{"KEY": "value"},
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
	if loaded.Sync != original.Sync {
		t.Errorf("Sync = %q, want %q", loaded.Sync, original.Sync)
	}
	if loaded.Harness != original.Harness {
		t.Errorf("Harness = %q, want %q", loaded.Harness, original.Harness)
	}
	if loaded.Task != original.Task {
		t.Errorf("Task = %q, want %q", loaded.Task, original.Task)
	}
	if loaded.Env["KEY"] != "value" {
		t.Errorf("Env[KEY] = %q, want %q", loaded.Env["KEY"], "value")
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
