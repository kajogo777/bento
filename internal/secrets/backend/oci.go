package backend

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"golang.org/x/crypto/nacl/box"
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
//   - Key stored as raw base64url encoding
type OCIBackend struct{}

func (b *OCIBackend) Name() string { return "oci" }

// Put encrypts the secrets map and returns the ciphertext and key in meta.
//
// Meta keys:
//   - "rawKey":     the one-time data key as raw base64url (for display/sharing)
//   - "ciphertext": base64url-encoded nonce+ciphertext (for packing into OCI layer)
func (b *OCIBackend) Put(ctx context.Context, key string, secrets map[string]string) (map[string]string, error) {
	plaintext, err := json.Marshal(secrets)
	if err != nil {
		return nil, fmt.Errorf("marshaling secrets: %w", err)
	}

	// Generate random data key.
	var dataKey [32]byte
	if _, err := rand.Read(dataKey[:]); err != nil {
		return nil, fmt.Errorf("generating data key: %w", err)
	}

	// Generate random nonce.
	var nonce [24]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, fmt.Errorf("generating nonce: %w", err)
	}

	// Encrypt: nonce is prepended to the ciphertext.
	encrypted := secretbox.Seal(nonce[:], plaintext, &nonce, &dataKey)

	keyStr := base64.RawURLEncoding.EncodeToString(dataKey[:])
	cipherStr := base64.RawURLEncoding.EncodeToString(encrypted)

	return map[string]string{
		"rawKey":     keyStr,
		"ciphertext": cipherStr,
	}, nil
}

// Get decrypts secrets from the ciphertext provided in opts.
//
// Required opts:
//   - "rawKey":     the one-time data key as raw base64url
//   - "ciphertext": base64url-encoded nonce+ciphertext
func (b *OCIBackend) Get(ctx context.Context, key string, opts map[string]string) (map[string]string, error) {
	keyStr := opts["rawKey"]
	if keyStr == "" {
		return nil, fmt.Errorf("oci backend: data key required — provide the key from save output")
	}

	cipherStr := opts["ciphertext"]
	if cipherStr == "" {
		return nil, fmt.Errorf("oci backend: no encrypted secrets layer found in checkpoint")
	}

	// Decode the raw base64url key directly.
	keyBytes, err := base64.RawURLEncoding.DecodeString(keyStr)
	if err != nil {
		return nil, fmt.Errorf("oci backend: decoding data key: %w", err)
	}
	if len(keyBytes) != 32 {
		return nil, fmt.Errorf("oci backend: invalid data key length (%d bytes, expected 32)", len(keyBytes))
	}
	var dataKey [32]byte
	copy(dataKey[:], keyBytes)

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
	plaintext, ok := secretbox.Open(nil, encrypted[24:], &nonce, &dataKey)
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
	display := "Secrets encrypted in checkpoint. Use key wrapping to share."
	persist := "This checkpoint has encrypted secrets. Use key wrapping to restore."
	return display, persist
}

// EncryptSecrets encrypts a placeholder→value map and returns the ciphertext
// blob and the raw 32-byte DEK. This is the single encryption entry point
// used by save and push.
func EncryptSecrets(secrets map[string]string) (ciphertext string, rawDEK [32]byte, err error) {
	be := &OCIBackend{}
	meta, err := be.Put(context.Background(), "", secrets)
	if err != nil {
		return "", [32]byte{}, err
	}
	// Parse the raw DEK bytes from the rawKey.
	keyStr := meta["rawKey"]
	keyBytes, decErr := base64.RawURLEncoding.DecodeString(keyStr)
	if decErr != nil {
		return "", [32]byte{}, fmt.Errorf("decoding raw DEK: %w", decErr)
	}
	copy(rawDEK[:], keyBytes)
	return meta["ciphertext"], rawDEK, nil
}

// DecryptSecrets decrypts a ciphertext blob using the provided raw DEK
// and returns the placeholder→value map. This is the single decryption entry
// point used by open, import, and secrets-file.
func DecryptSecrets(ciphertext string, rawDEK [32]byte) (map[string]string, error) {
	be := &OCIBackend{}
	// Encode rawDEK to base64 for the internal Get() call.
	keyStr := base64.RawURLEncoding.EncodeToString(rawDEK[:])
	return be.Get(context.Background(), "", map[string]string{
		"ciphertext": ciphertext,
		"rawKey":     keyStr,
	})
}

// --- Curve25519 key wrapping (BENTO-006) ---

// MultiRecipientEnvelope is the on-disk/in-OCI format for wrapped secrets.
type MultiRecipientEnvelope struct {
	Version     int               `json:"v"`
	Sender      string            `json:"sender"`                // "bento-pk-..." (sender's public key)
	Ciphertext  string            `json:"ciphertext"`
	WrappedKeys []WrappedKeyEntry `json:"wrappedKeys,omitempty"`
}

// WrappedKeyEntry holds a DEK wrapped to a single recipient.
type WrappedKeyEntry struct {
	Recipient  string `json:"recipient"`  // "bento-pk-..."
	WrappedDEK string `json:"wrappedDEK"` // base64url(72 bytes)
}

// WrapDEK wraps a 32-byte DEK to a recipient's Curve25519 public key
// using NaCl authenticated boxes (crypto_box).
//
// The sender's private key is required for authenticated encryption.
// Returns 72 bytes: 24-byte nonce || 48-byte box.Seal output.
func WrapDEK(dek [32]byte, recipientPub [32]byte, senderPriv [32]byte) ([]byte, error) {
	var nonce [24]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, fmt.Errorf("generating nonce: %w", err)
	}
	out := box.Seal(nonce[:], dek[:], &nonce, &recipientPub, &senderPriv)
	return out, nil // 24 + 32 + box.Overhead(16) = 72 bytes
}

// UnwrapDEK unwraps a DEK using the recipient's private key and the
// sender's public key (from the envelope's "sender" field).
//
// Input: 72 bytes (24-byte nonce || 48-byte ciphertext).
// Returns the 32-byte DEK or an error if decryption fails.
func UnwrapDEK(wrapped []byte, senderPub, recipientPub, recipientPriv [32]byte) ([32]byte, error) {
	if len(wrapped) != 72 {
		return [32]byte{}, fmt.Errorf("invalid wrapped DEK: expected 72 bytes, got %d", len(wrapped))
	}
	var nonce [24]byte
	copy(nonce[:], wrapped[:24])
	plaintext, ok := box.Open(nil, wrapped[24:], &nonce, &senderPub, &recipientPriv)
	if !ok {
		return [32]byte{}, fmt.Errorf("DEK unwrap failed — wrong key or corrupted data")
	}
	if len(plaintext) != 32 {
		return [32]byte{}, fmt.Errorf("invalid DEK: expected 32 bytes, got %d", len(plaintext))
	}
	var dek [32]byte
	copy(dek[:], plaintext)
	return dek, nil
}

// BuildMultiRecipientEnvelope creates a multi-recipient envelope by wrapping the
// DEK to each recipient using the sender's keypair.
func BuildMultiRecipientEnvelope(
	ciphertext string,
	rawDEK [32]byte,
	senderPub, senderPriv [32]byte,
	recipients [][32]byte,
) (*MultiRecipientEnvelope, error) {
	env := &MultiRecipientEnvelope{
		Version:    1,
		Sender:     "bento-pk-" + base64.RawURLEncoding.EncodeToString(senderPub[:]),
		Ciphertext: ciphertext,
	}

	for _, recipPub := range recipients {
		wrapped, err := WrapDEK(rawDEK, recipPub, senderPriv)
		if err != nil {
			return nil, fmt.Errorf("wrapping DEK for recipient: %w", err)
		}
		env.WrappedKeys = append(env.WrappedKeys, WrappedKeyEntry{
			Recipient:  "bento-pk-" + base64.RawURLEncoding.EncodeToString(recipPub[:]),
			WrappedDEK: base64.RawURLEncoding.EncodeToString(wrapped),
		})
	}

	return env, nil
}

// TryUnwrapEnvelope attempts to decrypt secrets from a multi-recipient envelope
// using the provided private key. It scans wrappedKeys for a matching recipient.
// Returns the decrypted secrets map, or an error if no matching key is found.
func TryUnwrapEnvelope(envelopeJSON []byte, recipientPub, recipientPriv [32]byte) (map[string]string, error) {
	var env MultiRecipientEnvelope
	if err := json.Unmarshal(envelopeJSON, &env); err != nil {
		return nil, fmt.Errorf("parsing envelope: %w", err)
	}

	if len(env.WrappedKeys) == 0 {
		return nil, fmt.Errorf("envelope has no wrapped keys")
	}

	// Parse sender public key.
	senderPubStr := env.Sender
	if senderPubStr == "" {
		return nil, fmt.Errorf("envelope missing sender public key")
	}
	const pkPrefix = "bento-pk-"
	if !strings.HasPrefix(senderPubStr, pkPrefix) {
		return nil, fmt.Errorf("invalid sender public key format")
	}
	senderPubBytes, err := base64.RawURLEncoding.DecodeString(senderPubStr[len(pkPrefix):])
	if err != nil || len(senderPubBytes) != 32 {
		return nil, fmt.Errorf("invalid sender public key")
	}
	var senderPub [32]byte
	copy(senderPub[:], senderPubBytes)

	// Find matching recipient.
	recipPubStr := "bento-pk-" + base64.RawURLEncoding.EncodeToString(recipientPub[:])
	for _, wk := range env.WrappedKeys {
		if wk.Recipient != recipPubStr {
			continue
		}
		wrapped, err := base64.RawURLEncoding.DecodeString(wk.WrappedDEK)
		if err != nil {
			return nil, fmt.Errorf("decoding wrapped DEK: %w", err)
		}
		dek, err := UnwrapDEK(wrapped, senderPub, recipientPub, recipientPriv)
		if err != nil {
			return nil, err
		}
		// Decrypt the secrets using the unwrapped DEK.
		return DecryptSecrets(env.Ciphertext, dek)
	}

	return nil, fmt.Errorf("no wrapped key found for this recipient")
}
