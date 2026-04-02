package backend

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestLocalBackend_PutGetDelete(t *testing.T) {
	tmp := t.TempDir()
	b := &LocalBackend{basePath: tmp}

	ctx := context.Background()
	key := "ws-test123/cp-1"
	secrets := map[string]string{
		"a1b2c3d4e5f6": "test-key-value-123",
		"f6e5d4c3b2a1": "test-token-value-456",
	}

	// Put
	meta, err := b.Put(ctx, key, secrets)
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}
	if meta != nil {
		t.Errorf("expected nil meta, got %v", meta)
	}

	// Verify file exists with correct permissions.
	path := filepath.Join(tmp, key+".json")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("secrets file not created: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("expected 0600 permissions, got %o", info.Mode().Perm())
	}

	// Get
	got, err := b.Get(ctx, key, nil)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 secrets, got %d", len(got))
	}
	if got["a1b2c3d4e5f6"] != "test-key-value-123" {
		t.Errorf("wrong value for a1b2c3d4e5f6: %q", got["a1b2c3d4e5f6"])
	}
	if got["f6e5d4c3b2a1"] != "test-token-value-456" {
		t.Errorf("wrong value for f6e5d4c3b2a1: %q", got["f6e5d4c3b2a1"])
	}

	// Delete
	err = b.Delete(ctx, key)
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("secrets file should be deleted")
	}
}

func TestLocalBackend_GetMissing(t *testing.T) {
	tmp := t.TempDir()
	b := &LocalBackend{basePath: tmp}

	ctx := context.Background()
	_, err := b.Get(ctx, "ws-nonexistent/cp-99", nil)
	if err == nil {
		t.Fatal("expected error for missing key")
	}
}

func TestLocalBackend_DeleteIdempotent(t *testing.T) {
	tmp := t.TempDir()
	b := &LocalBackend{basePath: tmp}

	ctx := context.Background()
	// Deleting a non-existent key should not error.
	err := b.Delete(ctx, "ws-nonexistent/cp-99")
	if err != nil {
		t.Fatalf("Delete of non-existent key should not error: %v", err)
	}
}

func TestLocalBackend_PutIdempotent(t *testing.T) {
	tmp := t.TempDir()
	b := &LocalBackend{basePath: tmp}

	ctx := context.Background()
	key := "ws-test/cp-1"

	// First put
	_, err := b.Put(ctx, key, map[string]string{"a": "1"})
	if err != nil {
		t.Fatalf("first Put failed: %v", err)
	}

	// Second put overwrites
	_, err = b.Put(ctx, key, map[string]string{"b": "2"})
	if err != nil {
		t.Fatalf("second Put failed: %v", err)
	}

	got, err := b.Get(ctx, key, nil)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if _, ok := got["a"]; ok {
		t.Error("first put's data should be overwritten")
	}
	if got["b"] != "2" {
		t.Errorf("expected b=2, got %q", got["b"])
	}
}

func TestLocalBackend_Available(t *testing.T) {
	b := &LocalBackend{}
	if !b.Available() {
		t.Error("local backend should always be available")
	}
}

func TestLocalBackend_Hint(t *testing.T) {
	b := &LocalBackend{}
	display, persist := b.Hint("ws-abc/cp-3", nil)

	if display == "" || persist == "" {
		t.Error("hints should not be empty")
	}
	if !contains(display, "push --include-secrets") {
		t.Errorf("display hint should mention push: %q", display)
	}
	if !contains(persist, "push --include-secrets") {
		t.Errorf("persist hint should mention push: %q", persist)
	}
}

func TestLocalBackend_DeleteCleansEmptyDir(t *testing.T) {
	tmp := t.TempDir()
	b := &LocalBackend{basePath: tmp}

	ctx := context.Background()
	key := "ws-cleanup/cp-1"

	_, err := b.Put(ctx, key, map[string]string{"a": "1"})
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	err = b.Delete(ctx, key)
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Parent directory should be cleaned up.
	wsDir := filepath.Join(tmp, "ws-cleanup")
	if _, err := os.Stat(wsDir); !os.IsNotExist(err) {
		t.Error("empty workspace directory should be removed")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStr(s, substr))
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
