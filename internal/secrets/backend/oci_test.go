package backend

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"golang.org/x/crypto/nacl/box"
	"golang.org/x/crypto/nacl/secretbox"
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
	if meta["rawKey"] == "" {
		t.Fatal("expected rawKey in meta")
	}
	if meta["ciphertext"] == "" {
		t.Fatal("expected ciphertext in meta")
	}
	if !strings.HasPrefix(meta["rawKey"], "A") && !strings.HasPrefix(meta["rawKey"], "B") {
		// Just verify it's valid base64url, not checking for bento-dk- prefix
		_, err := base64.RawURLEncoding.DecodeString(meta["rawKey"])
		if err != nil {
			t.Errorf("rawKey should be valid base64url, got %q", meta["rawKey"])
		}
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
	meta["rawKey"] = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
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
		"rawKey": "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
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

	if meta1["rawKey"] == meta2["rawKey"] {
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
	display, persist := b.Hint("ws-test/cp-1", nil)

	if !strings.Contains(display, "key wrapping") {
		t.Errorf("display hint should mention key wrapping: %q", display)
	}
	if !strings.Contains(persist, "key wrapping") {
		t.Errorf("persist hint should mention key wrapping: %q", persist)
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
		"rawKey":     "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		"ciphertext": "dG9vc2hvcnQ", // "tooshort" base64url
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

	// Key without valid base64url encoding.
	_, err := b.Get(ctx, "ws-test/cp-1", map[string]string{
		"rawKey":     "not-a-valid-base64url!!!",
		"ciphertext": "dGVzdA",
	})
	if err == nil {
		t.Fatal("expected error for invalid key format")
	}

	// Key with wrong length (16 bytes instead of 32).
	_, err = b.Get(ctx, "ws-test/cp-1", map[string]string{
		"rawKey":     "AAAAAAAAAAAAAAAA",
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

	ciphertext, rawDEK, err := EncryptSecrets(secrets)
	if err != nil {
		t.Fatalf("EncryptSecrets failed: %v", err)
	}

	if rawDEK == [32]byte{} {
		t.Error("rawDEK should not be zero")
	}

	if ciphertext == "" {
		t.Error("ciphertext should not be empty")
	}

	// Decrypt using the raw DEK directly
	var nonce [24]byte
	encrypted, _ := base64.RawURLEncoding.DecodeString(ciphertext)
	copy(nonce[:], encrypted[:24])
	plaintext, ok := secretbox.Open(nil, encrypted[24:], &nonce, &rawDEK)
	if !ok {
		t.Fatal("decryption failed")
	}

	var got map[string]string
	if err := json.Unmarshal(plaintext, &got); err != nil {
		t.Fatalf("unmarshaling decrypted secrets: %v", err)
	}

	for k, v := range secrets {
		if got[k] != v {
			t.Errorf("key %q: got %q, want %q", k, got[k], v)
		}
	}
}

// --- Curve25519 key wrapping tests ---

func TestWrapUnwrapDEK_RoundTrip(t *testing.T) {
	// Generate sender and recipient keypairs.
	senderPub, senderPriv, _ := box.GenerateKey(rand.Reader)
	recipPub, recipPriv, _ := box.GenerateKey(rand.Reader)

	// Use a fixed DEK.
	var dek [32]byte
	dek[0] = 0x42
	dek[31] = 0xFF

	wrapped, err := WrapDEK(dek, *recipPub, *senderPriv)
	if err != nil {
		t.Fatalf("WrapDEK failed: %v", err)
	}
	if len(wrapped) != 72 {
		t.Fatalf("expected 72 bytes, got %d", len(wrapped))
	}

	got, err := UnwrapDEK(wrapped, *senderPub, *recipPub, *recipPriv)
	if err != nil {
		t.Fatalf("UnwrapDEK failed: %v", err)
	}
	if got != dek {
		t.Error("unwrapped DEK mismatch")
	}
}

func TestWrapDEK_WrongRecipientKey(t *testing.T) {
	senderPub, senderPriv, _ := box.GenerateKey(rand.Reader)
	recipPub, _, _ := box.GenerateKey(rand.Reader)
	_, wrongPriv, _ := box.GenerateKey(rand.Reader) // different recipient

	var dek [32]byte
	dek[0] = 0x42

	wrapped, _ := WrapDEK(dek, *recipPub, *senderPriv)
	_, err := UnwrapDEK(wrapped, *senderPub, *recipPub, *wrongPriv)
	if err == nil {
		t.Fatal("expected error unwrapping with wrong key")
	}
}

func TestUnwrapDEK_InvalidLength(t *testing.T) {
	_, err := UnwrapDEK([]byte("short"), [32]byte{}, [32]byte{}, [32]byte{})
	if err == nil {
		t.Fatal("expected error for invalid length")
	}
}

func TestBuildMultiRecipientEnvelope(t *testing.T) {
	secrets := map[string]string{"a": "secret-val"}
	ciphertext, rawDEK, err := EncryptSecrets(secrets)
	if err != nil {
		t.Fatalf("EncryptSecrets failed: %v", err)
	}

	senderPub, senderPriv, _ := box.GenerateKey(rand.Reader)
	recip1Pub, _, _ := box.GenerateKey(rand.Reader)
	recip2Pub, _, _ := box.GenerateKey(rand.Reader)

	env, err := BuildMultiRecipientEnvelope(ciphertext, rawDEK, *senderPub, *senderPriv, [][32]byte{*recip1Pub, *recip2Pub})
	if err != nil {
		t.Fatalf("BuildMultiRecipientEnvelope failed: %v", err)
	}
	if env.Version != 1 {
		t.Errorf("expected version 1, got %d", env.Version)
	}
	if len(env.WrappedKeys) != 2 {
		t.Fatalf("expected 2 wrapped keys, got %d", len(env.WrappedKeys))
	}
	if env.Sender == "" {
		t.Error("sender should not be empty")
	}
}

func TestTryUnwrapEnvelope_RoundTrip(t *testing.T) {
	secrets := map[string]string{"a": "secret-val", "b": "another"}
	ciphertext, rawDEK, _ := EncryptSecrets(secrets)

	senderPub, senderPriv, _ := box.GenerateKey(rand.Reader)
	recipPub, recipPriv, _ := box.GenerateKey(rand.Reader)

	env, _ := BuildMultiRecipientEnvelope(ciphertext, rawDEK, *senderPub, *senderPriv, [][32]byte{*recipPub})

	envJSON, _ := json.Marshal(env)
	got, err := TryUnwrapEnvelope(envJSON, *recipPub, *recipPriv)
	if err != nil {
		t.Fatalf("TryUnwrapEnvelope failed: %v", err)
	}
	if got["a"] != "secret-val" {
		t.Errorf("expected 'secret-val', got %q", got["a"])
	}
	if got["b"] != "another" {
		t.Errorf("expected 'another', got %q", got["b"])
	}
}

func TestTryUnwrapEnvelope_NonRecipientFails(t *testing.T) {
	secrets := map[string]string{"a": "val"}
	ciphertext, rawDEK, _ := EncryptSecrets(secrets)

	senderPub, senderPriv, _ := box.GenerateKey(rand.Reader)
	recipPub, _, _ := box.GenerateKey(rand.Reader)
	outsiderPub, outsiderPriv, _ := box.GenerateKey(rand.Reader)

	env, _ := BuildMultiRecipientEnvelope(ciphertext, rawDEK, *senderPub, *senderPriv, [][32]byte{*recipPub})
	envJSON, _ := json.Marshal(env)

	_, err := TryUnwrapEnvelope(envJSON, *outsiderPub, *outsiderPriv)
	if err == nil {
		t.Fatal("expected error for non-recipient")
	}
}

func TestTryUnwrapEnvelope_NoWrappedKeys(t *testing.T) {
	env := MultiRecipientEnvelope{Version: 1, Sender: "bento-pk-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", Ciphertext: "x"}
	envJSON, _ := json.Marshal(env)
	_, err := TryUnwrapEnvelope(envJSON, [32]byte{}, [32]byte{})
	if err == nil {
		t.Fatal("expected error for empty wrappedKeys")
	}
}
