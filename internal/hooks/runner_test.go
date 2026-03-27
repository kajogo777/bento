package hooks

import (
	"strings"
	"testing"
)

func TestRun_Success(t *testing.T) {
	r := NewRunner(t.TempDir(), 10)
	err := r.Run("test_hook", "echo hello")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestRun_Failure(t *testing.T) {
	r := NewRunner(t.TempDir(), 10)
	err := r.Run("test_hook", "exit 1")
	if err == nil {
		t.Fatal("expected error for failing command")
	}
	if !strings.Contains(err.Error(), "failed") {
		t.Errorf("error should contain 'failed', got: %v", err)
	}
}

func TestRun_Timeout(t *testing.T) {
	r := NewRunner(t.TempDir(), 1)
	err := r.Run("test_hook", "sleep 10")
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("error should contain 'timed out', got: %v", err)
	}
}

func TestRun_WorkDir(t *testing.T) {
	dir := t.TempDir()
	r := NewRunner(dir, 10)
	// pwd should output the temp dir; just verify no error occurs.
	err := r.Run("test_hook", "pwd")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestRun_HookNameInError(t *testing.T) {
	r := NewRunner(t.TempDir(), 10)
	err := r.Run("my_hook", "exit 1")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "my_hook") {
		t.Errorf("error should reference hook name, got: %v", err)
	}
}
