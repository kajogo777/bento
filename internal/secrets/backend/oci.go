package backend

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"

	"golang.org/x/crypto/nacl/secretbox"
)

// OCIBackend stores secrets as an encrypted blob that gets packed into an
// OCI layer by the save flow. The encryption key is generated per checkpoint
// and displayed to the user for out-of-band sharing.
//
// Unlike other backends, OCIBackend doesn't write to an external store.
// Instead, Put() returns the encrypted blob and key in meta, and the caller
// (save_core.go) is responsible for packing it as an OCI layer. Get() receives
// the encrypted blob and key via opts and decrypts in-memory.
//
// Encryption: NaCl secretbox (XSalsa20-Poly1305)
//   - 32-byte random key per checkpoint
//   - 24-byte random nonce prepended to ciphertext
//   - Authenticated encryption (tamper detection)
//   - Key displayed as "bento-sk-<base64url>"
type OCIBackend struct{}

func (b *OCIBackend) Name() string { return "oci" }

// Put encrypts the secrets map and returns the ciphertext and key in meta.
//
// Meta keys:
//   - "secretKey":  the one-time key as "bento-sk-<base64url>" (for display/sharing)
//   - "ciphertext": base64url-encoded nonce+ciphertext (for packing into OCI layer)
func (b *OCIBackend) Put(ctx context.Context, key string, secrets map[string]string) (map[string]string, error) {
	plaintext, err := json.Marshal(secrets)
	if err != nil {
		return nil, fmt.Errorf("marshaling secrets: %w", err)
	}

	// Generate random key.
	var secretKey [32]byte
	if _, err := rand.Read(secretKey[:]); err != nil {
		return nil, fmt.Errorf("generating secret key: %w", err)
	}

	// Generate random nonce.
	var nonce [24]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, fmt.Errorf("generating nonce: %w", err)
	}

	// Encrypt: nonce is prepended to the ciphertext.
	encrypted := secretbox.Seal(nonce[:], plaintext, &nonce, &secretKey)

	keyStr := "bento-sk-" + base64.RawURLEncoding.EncodeToString(secretKey[:])
	cipherStr := base64.RawURLEncoding.EncodeToString(encrypted)

	return map[string]string{
		"secretKey":  keyStr,
		"ciphertext": cipherStr,
	}, nil
}

// Get decrypts secrets from the ciphertext provided in opts.
//
// Required opts:
//   - "secretKey":  the one-time key as "bento-sk-<base64url>"
//   - "ciphertext": base64url-encoded nonce+ciphertext
func (b *OCIBackend) Get(ctx context.Context, key string, opts map[string]string) (map[string]string, error) {
	keyStr := opts["secretKey"]
	if keyStr == "" {
		return nil, fmt.Errorf("oci backend: secret key required — use --secret-key flag or BENTO_SECRET_KEY env var")
	}

	cipherStr := opts["ciphertext"]
	if cipherStr == "" {
		return nil, fmt.Errorf("oci backend: no encrypted secrets layer found in checkpoint")
	}

	// Parse key.
	const prefix = "bento-sk-"
	if len(keyStr) <= len(prefix) {
		return nil, fmt.Errorf("oci backend: invalid secret key format")
	}
	keyBytes, err := base64.RawURLEncoding.DecodeString(keyStr[len(prefix):])
	if err != nil {
		return nil, fmt.Errorf("oci backend: decoding secret key: %w", err)
	}
	if len(keyBytes) != 32 {
		return nil, fmt.Errorf("oci backend: invalid secret key length (%d bytes, expected 32)", len(keyBytes))
	}
	var secretKey [32]byte
	copy(secretKey[:], keyBytes)

	// Decode ciphertext.
	encrypted, err := base64.RawURLEncoding.DecodeString(cipherStr)
	if err != nil {
		return nil, fmt.Errorf("oci backend: decoding ciphertext: %w", err)
	}

	// Extract nonce (first 24 bytes).
	if len(encrypted) < 24 {
		return nil, fmt.Errorf("oci backend: ciphertext too short")
	}
	var nonce [24]byte
	copy(nonce[:], encrypted[:24])

	// Decrypt.
	plaintext, ok := secretbox.Open(nil, encrypted[24:], &nonce, &secretKey)
	if !ok {
		return nil, fmt.Errorf("oci backend: decryption failed — wrong key or corrupted data")
	}

	var secrets map[string]string
	if err := json.Unmarshal(plaintext, &secrets); err != nil {
		return nil, fmt.Errorf("oci backend: parsing decrypted secrets: %w", err)
	}

	return secrets, nil
}

// Delete is a no-op for the OCI backend — secrets are stored in the OCI
// layer and removed when the checkpoint is garbage collected.
func (b *OCIBackend) Delete(ctx context.Context, key string) error {
	return nil
}

// Available always returns true — no external dependencies.
func (b *OCIBackend) Available() bool { return true }

func (b *OCIBackend) Hint(key string, meta map[string]string) (string, string) {
	secretKey := meta["secretKey"]

	display := fmt.Sprintf("Secrets encrypted in checkpoint. To restore:\n   bento open <ref> --secret-key %s", secretKey)
	persist := "This checkpoint has encrypted secrets. Re-open with:\n   bento open <ref> --secret-key <KEY>\n   Ask the sender for the secret key."
	return display, persist
}

// EncryptSecrets encrypts a placeholder→value map and returns the ciphertext
// blob and the one-time secret key. This is the single encryption entry point
// used by save, push, and export.
func EncryptSecrets(secrets map[string]string) (ciphertext string, secretKey string, err error) {
	be := &OCIBackend{}
	meta, err := be.Put(context.Background(), "", secrets)
	if err != nil {
		return "", "", err
	}
	return meta["ciphertext"], meta["secretKey"], nil
}

// DecryptSecrets decrypts a ciphertext blob using the provided secret key
// and returns the placeholder→value map. This is the single decryption entry
// point used by open, import, and secrets-file.
func DecryptSecrets(ciphertext string, secretKey string) (map[string]string, error) {
	be := &OCIBackend{}
	return be.Get(context.Background(), "", map[string]string{
		"ciphertext": ciphertext,
		"secretKey":  secretKey,
	})
}
