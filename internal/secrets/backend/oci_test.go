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

func TestOCIBackend_TamperedCiphertext(t *testing.T) {
	b := &OCIBackend{}
	ctx := context.Background()
	secrets := map[string]string{"a": "secret-value"}

	meta, err := b.Put(ctx, "ws-test/cp-1", secrets)
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// Tamper with the ciphertext by flipping a character.
	ct := meta["ciphertext"]
	if len(ct) < 10 {
		t.Fatal("ciphertext too short to tamper")
	}
	tampered := ct[:5] + "X" + ct[6:]
	meta["ciphertext"] = tampered

	_, err = b.Get(ctx, "ws-test/cp-1", meta)
	if err == nil {
		t.Fatal("expected error for tampered ciphertext")
	}
	if !strings.Contains(err.Error(), "decryption failed") && !strings.Contains(err.Error(), "decoding") {
		t.Errorf("expected decryption or decoding error, got: %v", err)
	}
}

func TestOCIBackend_EmptySecrets(t *testing.T) {
	b := &OCIBackend{}
	ctx := context.Background()

	meta, err := b.Put(ctx, "ws-test/cp-1", map[string]string{})
	if err != nil {
		t.Fatalf("Put with empty secrets failed: %v", err)
	}

	got, err := b.Get(ctx, "ws-test/cp-1", meta)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty map, got %d entries", len(got))
	}
}

func TestOCIBackend_TruncatedCiphertext(t *testing.T) {
	b := &OCIBackend{}
	ctx := context.Background()

	// Ciphertext shorter than the 24-byte nonce.
	_, err := b.Get(ctx, "ws-test/cp-1", map[string]string{
		"secretKey":  "bento-sk-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		"ciphertext": "dG9vc2hvcnQ",  // "tooshort" base64url
	})
	if err == nil {
		t.Fatal("expected error for truncated ciphertext")
	}
	if !strings.Contains(err.Error(), "too short") {
		t.Errorf("expected 'too short' error, got: %v", err)
	}
}

func TestOCIBackend_InvalidKeyFormat(t *testing.T) {
	b := &OCIBackend{}
	ctx := context.Background()

	// Key without the bento-sk- prefix.
	_, err := b.Get(ctx, "ws-test/cp-1", map[string]string{
		"secretKey":  "not-a-valid-key",
		"ciphertext": "dGVzdA",
	})
	if err == nil {
		t.Fatal("expected error for invalid key format")
	}

	// Key with prefix but wrong length (16 bytes instead of 32).
	_, err = b.Get(ctx, "ws-test/cp-1", map[string]string{
		"secretKey":  "bento-sk-AAAAAAAAAAAAAAAAAAAAAA",
		"ciphertext": "dGVzdA",
	})
	if err == nil {
		t.Fatal("expected error for wrong key length")
	}
}

func TestEncryptDecryptSecrets_RoundTrip(t *testing.T) {
	secrets := map[string]string{
		"placeholder1": "value1",
		"placeholder2": "value2",
	}

	ciphertext, secretKey, err := EncryptSecrets(secrets)
	if err != nil {
		t.Fatalf("EncryptSecrets failed: %v", err)
	}

	got, err := DecryptSecrets(ciphertext, secretKey)
	if err != nil {
		t.Fatalf("DecryptSecrets failed: %v", err)
	}

	for k, v := range secrets {
		if got[k] != v {
			t.Errorf("key %q: got %q, want %q", k, got[k], v)
		}
	}
}
