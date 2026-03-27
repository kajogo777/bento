package registry

import (
	"path/filepath"
	"testing"
)

func TestParseRef_StoreAndTag(t *testing.T) {
	storeName, tag, err := ParseRef("myproject:cp-3")
	if err != nil {
		t.Fatalf("ParseRef failed: %v", err)
	}
	if storeName != "myproject" {
		t.Errorf("storeName: got %q, want %q", storeName, "myproject")
	}
	if tag != "cp-3" {
		t.Errorf("tag: got %q, want %q", tag, "cp-3")
	}
}

func TestParseRef_BareTag(t *testing.T) {
	// "cp-3" looks like a tag, so storeName should be derived from cwd.
	storeName, tag, err := ParseRef("cp-3")
	if err != nil {
		t.Fatalf("ParseRef failed: %v", err)
	}
	// storeName should be the current directory name.
	expectedStore := currentDirBaseName(t)
	if storeName != expectedStore {
		t.Errorf("storeName: got %q, want %q", storeName, expectedStore)
	}
	if tag != "cp-3" {
		t.Errorf("tag: got %q, want %q", tag, "cp-3")
	}
}

func TestParseRef_Empty(t *testing.T) {
	storeName, tag, err := ParseRef("")
	if err != nil {
		t.Fatalf("ParseRef failed: %v", err)
	}
	expectedStore := currentDirBaseName(t)
	if storeName != expectedStore {
		t.Errorf("storeName: got %q, want %q", storeName, expectedStore)
	}
	if tag != "latest" {
		t.Errorf("tag: got %q, want %q", tag, "latest")
	}
}

func TestParseRef_StoreNameOnly(t *testing.T) {
	// "myproject" without colon and doesn't look like a tag -> treated as store name.
	storeName, tag, err := ParseRef("myproject")
	if err != nil {
		t.Fatalf("ParseRef failed: %v", err)
	}
	if storeName != "myproject" {
		t.Errorf("storeName: got %q, want %q", storeName, "myproject")
	}
	if tag != "latest" {
		t.Errorf("tag: got %q, want %q", tag, "latest")
	}
}

func TestParseRef_LatestTag(t *testing.T) {
	// "latest" looks like a tag.
	storeName, tag, err := ParseRef("latest")
	if err != nil {
		t.Fatalf("ParseRef failed: %v", err)
	}
	expectedStore := currentDirBaseName(t)
	if storeName != expectedStore {
		t.Errorf("storeName: got %q, want %q", storeName, expectedStore)
	}
	if tag != "latest" {
		t.Errorf("tag: got %q, want %q", tag, "latest")
	}
}

func TestParseRef_CheckpointPrefix(t *testing.T) {
	storeName, tag, err := ParseRef("checkpoint-1")
	if err != nil {
		t.Fatalf("ParseRef failed: %v", err)
	}
	expectedStore := currentDirBaseName(t)
	if storeName != expectedStore {
		t.Errorf("storeName: got %q, want %q", storeName, expectedStore)
	}
	if tag != "checkpoint-1" {
		t.Errorf("tag: got %q, want %q", tag, "checkpoint-1")
	}
}

func TestParseRef_StoreWithLatestTag(t *testing.T) {
	storeName, tag, err := ParseRef("myproject:latest")
	if err != nil {
		t.Fatalf("ParseRef failed: %v", err)
	}
	if storeName != "myproject" {
		t.Errorf("storeName: got %q, want %q", storeName, "myproject")
	}
	if tag != "latest" {
		t.Errorf("tag: got %q, want %q", tag, "latest")
	}
}

// currentDirBaseName is a test helper that returns filepath.Base of the working directory.
func currentDirBaseName(t *testing.T) string {
	t.Helper()
	// We use the same logic as the package under test.
	name, err := currentDirName()
	if err != nil {
		t.Fatalf("currentDirName failed: %v", err)
	}
	return filepath.Base(name)
}
