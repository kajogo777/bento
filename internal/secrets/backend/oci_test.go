package backend

import (
	"context"
	"strings"
	"testing"
)

func TestOCIBackend_PutGetRoundTrip(t *testing.T) {
	b := &OCIBackend{}
	ctx := context.Background()
	key := "ws-test/cp-1"
	secrets := map[string]string{
		"a1b2c3d4e5f6": "test-key-value-123",
		"f6e5d4c3b2a1": "test-token-value-456",
	}

	// Put — encrypt
	meta, err := b.Put(ctx, key, secrets)
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}
	if meta["secretKey"] == "" {
		t.Fatal("expected secretKey in meta")
	}
	if meta["ciphertext"] == "" {
		t.Fatal("expected ciphertext in meta")
	}
	if !strings.HasPrefix(meta["secretKey"], "bento-sk-") {
		t.Errorf("secretKey should start with bento-sk-, got %q", meta["secretKey"])
	}

	// Get — decrypt
	got, err := b.Get(ctx, key, meta)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 secrets, got %d", len(got))
	}
	if got["a1b2c3d4e5f6"] != "test-key-value-123" {
		t.Errorf("wrong value: %q", got["a1b2c3d4e5f6"])
	}
	if got["f6e5d4c3b2a1"] != "test-token-value-456" {
		t.Errorf("wrong value: %q", got["f6e5d4c3b2a1"])
	}
}

func TestOCIBackend_WrongKey(t *testing.T) {
	b := &OCIBackend{}
	ctx := context.Background()

	secrets := map[string]string{"a": "secret"}
	meta, err := b.Put(ctx, "ws-test/cp-1", secrets)
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// Tamper with the key.
	meta["secretKey"] = "bento-sk-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	_, err = b.Get(ctx, "ws-test/cp-1", meta)
	if err == nil {
		t.Fatal("expected decryption error with wrong key")
	}
	if !strings.Contains(err.Error(), "decryption failed") {
		t.Errorf("expected 'decryption failed' error, got: %v", err)
	}
}

func TestOCIBackend_MissingKey(t *testing.T) {
	b := &OCIBackend{}
	ctx := context.Background()

	_, err := b.Get(ctx, "ws-test/cp-1", map[string]string{
		"ciphertext": "something",
	})
	if err == nil {
		t.Fatal("expected error for missing key")
	}
}

func TestOCIBackend_MissingCiphertext(t *testing.T) {
	b := &OCIBackend{}
	ctx := context.Background()

	_, err := b.Get(ctx, "ws-test/cp-1", map[string]string{
		"secretKey": "bento-sk-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
	})
	if err == nil {
		t.Fatal("expected error for missing ciphertext")
	}
}

func TestOCIBackend_UniqueKeysPerPut(t *testing.T) {
	b := &OCIBackend{}
	ctx := context.Background()
	secrets := map[string]string{"a": "1"}

	meta1, _ := b.Put(ctx, "ws-test/cp-1", secrets)
	meta2, _ := b.Put(ctx, "ws-test/cp-2", secrets)

	if meta1["secretKey"] == meta2["secretKey"] {
		t.Error("each Put should generate a unique key")
	}
}

func TestOCIBackend_Available(t *testing.T) {
	b := &OCIBackend{}
	if !b.Available() {
		t.Error("oci backend should always be available")
	}
}

func TestOCIBackend_Hint(t *testing.T) {
	b := &OCIBackend{}
	meta := map[string]string{"secretKey": "bento-sk-testkey123"}

	display, persist := b.Hint("ws-test/cp-1", meta)

	if !strings.Contains(display, "bento-sk-testkey123") {
		t.Errorf("display hint should contain the actual key: %q", display)
	}
	if strings.Contains(persist, "bento-sk-testkey123") {
		t.Errorf("persist hint must NOT contain the actual key: %q", persist)
	}
	if !strings.Contains(persist, "--secret-key") {
		t.Errorf("persist hint should mention --secret-key flag: %q", persist)
	}
}

func TestOCIBackend_DeleteNoop(t *testing.T) {
	b := &OCIBackend{}
	err := b.Delete(context.Background(), "ws-test/cp-1")
	if err != nil {
		t.Fatalf("Delete should be no-op: %v", err)
	}
}
