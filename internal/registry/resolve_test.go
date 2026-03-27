package registry

import (
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
	// "cp-3" has no colon, so storeName should be empty and tag should be the ref.
	storeName, tag, err := ParseRef("cp-3")
	if err != nil {
		t.Fatalf("ParseRef failed: %v", err)
	}
	if storeName != "" {
		t.Errorf("storeName: got %q, want %q", storeName, "")
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
	if storeName != "" {
		t.Errorf("storeName: got %q, want %q", storeName, "")
	}
	if tag != "latest" {
		t.Errorf("tag: got %q, want %q", tag, "latest")
	}
}

func TestParseRef_BareString(t *testing.T) {
	// "postgres-done" without colon -> treated as tag, not store name.
	storeName, tag, err := ParseRef("postgres-done")
	if err != nil {
		t.Fatalf("ParseRef failed: %v", err)
	}
	if storeName != "" {
		t.Errorf("storeName: got %q, want %q", storeName, "")
	}
	if tag != "postgres-done" {
		t.Errorf("tag: got %q, want %q", tag, "postgres-done")
	}
}

func TestParseRef_LatestTag(t *testing.T) {
	// "latest" has no colon -> storeName empty, tag "latest".
	storeName, tag, err := ParseRef("latest")
	if err != nil {
		t.Fatalf("ParseRef failed: %v", err)
	}
	if storeName != "" {
		t.Errorf("storeName: got %q, want %q", storeName, "")
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
	if storeName != "" {
		t.Errorf("storeName: got %q, want %q", storeName, "")
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

func TestParseRef_StoreWithCustomTag(t *testing.T) {
	storeName, tag, err := ParseRef("myproject:postgres-done")
	if err != nil {
		t.Fatalf("ParseRef failed: %v", err)
	}
	if storeName != "myproject" {
		t.Errorf("storeName: got %q, want %q", storeName, "myproject")
	}
	if tag != "postgres-done" {
		t.Errorf("tag: got %q, want %q", tag, "postgres-done")
	}
}
