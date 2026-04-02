// Package keys manages Curve25519 keypairs for secret sharing via NaCl crypto_box.
//
// Key format:
//   - bento-pk-<base64url(32 bytes)>  — public key
//   - bento-sk-<base64url(32 bytes)>  — private key
//
// Keys are stored as JSON files in a platform-specific directory:
//   - macOS:   ~/.bento/keys/
//   - Linux:   ~/.local/share/bento/keys/ (or $XDG_DATA_HOME/bento/keys/)
//   - Windows: %LOCALAPPDATA%\bento\keys\
package keys

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"golang.org/x/crypto/curve25519"
	"golang.org/x/crypto/nacl/box"
)

// Key prefixes.
const (
	PrefixPublicKey  = "bento-pk-"
	PrefixPrivateKey = "bento-sk-"
)

// ErrNoKeypair is returned when no keypair is found on disk.
var ErrNoKeypair = errors.New("no keypair found")

// KeypairFile is the on-disk JSON format for a keypair.
type KeypairFile struct {
	Name       string `json:"name"`
	PublicKey  string `json:"publicKey"`
	PrivateKey string `json:"privateKey"`
	Created    string `json:"created"`
}

// GenerateKeypair creates a new Curve25519 keypair using crypto/rand.
// Returns the public and private keys as 32-byte arrays.
func GenerateKeypair() (publicKey, privateKey [32]byte, err error) {
	pub, priv, err := box.GenerateKey(rand.Reader)
	if err != nil {
		return [32]byte{}, [32]byte{}, fmt.Errorf("generating keypair: %w", err)
	}
	return *pub, *priv, nil
}

// DerivePublicKey computes the Curve25519 public key from a private key.
func DerivePublicKey(priv [32]byte) ([32]byte, error) {
	pub, err := curve25519.X25519(priv[:], curve25519.Basepoint)
	if err != nil {
		return [32]byte{}, fmt.Errorf("deriving public key: %w", err)
	}
	var pubKey [32]byte
	copy(pubKey[:], pub)
	return pubKey, nil
}

// FormatPublicKey encodes a public key as "bento-pk-<base64url>".
func FormatPublicKey(key [32]byte) string {
	return PrefixPublicKey + base64.RawURLEncoding.EncodeToString(key[:])
}

// FormatPrivateKey encodes a private key as "bento-sk-<base64url>".
func FormatPrivateKey(key [32]byte) string {
	return PrefixPrivateKey + base64.RawURLEncoding.EncodeToString(key[:])
}

// ParsePublicKey decodes a "bento-pk-..." string to 32 bytes.
// Returns an error if the prefix is wrong or the key is not 32 bytes.
func ParsePublicKey(s string) ([32]byte, error) {
	return parseKey(s, PrefixPublicKey, "public key")
}

// ParsePrivateKey decodes a "bento-sk-..." string to 32 bytes.
// Returns an error if the prefix is wrong or the key is not 32 bytes.
func ParsePrivateKey(s string) ([32]byte, error) {
	return parseKey(s, PrefixPrivateKey, "private key")
}

func parseKey(s, prefix, label string) ([32]byte, error) {
	if !strings.HasPrefix(s, prefix) {
		return [32]byte{}, fmt.Errorf("invalid %s: must start with %q", label, prefix)
	}
	b, err := base64.RawURLEncoding.DecodeString(s[len(prefix):])
	if err != nil {
		return [32]byte{}, fmt.Errorf("invalid %s: %w", label, err)
	}
	if len(b) != 32 {
		return [32]byte{}, fmt.Errorf("invalid %s: expected 32 bytes, got %d", label, len(b))
	}
	var key [32]byte
	copy(key[:], b)
	return key, nil
}

// DefaultKeysDir returns the platform-specific keys directory.
// If BENTO_KEYS_DIR is set, it takes precedence over the platform default.
func DefaultKeysDir() string {
	if dir := os.Getenv("BENTO_KEYS_DIR"); dir != "" {
		return dir
	}
	switch runtime.GOOS {
	case "linux":
		if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
			return filepath.Join(xdg, "bento", "keys")
		}
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".local", "share", "bento", "keys")
	case "windows":
		if localAppData := os.Getenv("LOCALAPPDATA"); localAppData != "" {
			return filepath.Join(localAppData, "bento", "keys")
		}
		home, _ := os.UserHomeDir()
		return filepath.Join(home, "AppData", "Local", "bento", "keys")
	default: // darwin
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".bento", "keys")
	}
}

// SaveKeypair writes a keypair to the keys directory.
// Creates the directory (0700) and file (0600) if they don't exist.
func SaveKeypair(name string, pub, priv [32]byte) error {
	return SaveKeypairTo(DefaultKeysDir(), name, pub, priv)
}

// SaveKeypairTo writes a keypair to a specific keys directory.
// Returns an error if a keypair with the same name already exists.
func SaveKeypairTo(keysDir, name string, pub, priv [32]byte) error {
	if err := os.MkdirAll(keysDir, 0700); err != nil {
		return fmt.Errorf("creating keys directory: %w", err)
	}

	path := filepath.Join(keysDir, name+".json")
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("keypair %q already exists at %s", name, path)
	}

	kf := KeypairFile{
		Name:       name,
		PublicKey:  FormatPublicKey(pub),
		PrivateKey: FormatPrivateKey(priv),
		Created:    time.Now().UTC().Format(time.RFC3339),
	}

	data, err := json.MarshalIndent(kf, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling keypair: %w", err)
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("writing keypair file: %w", err)
	}
	return nil
}

// LoadKeypair loads a named keypair from the keys directory.
func LoadKeypair(name string) (pub, priv [32]byte, err error) {
	return LoadKeypairFrom(DefaultKeysDir(), name)
}

// LoadKeypairFrom loads a named keypair from a specific keys directory.
func LoadKeypairFrom(keysDir, name string) (pub, priv [32]byte, err error) {
	path := filepath.Join(keysDir, name+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return [32]byte{}, [32]byte{}, fmt.Errorf("keypair %q not found at %s", name, path)
		}
		return [32]byte{}, [32]byte{}, fmt.Errorf("reading keypair: %w", err)
	}

	var kf KeypairFile
	if err := json.Unmarshal(data, &kf); err != nil {
		return [32]byte{}, [32]byte{}, fmt.Errorf("parsing keypair file: %w", err)
	}

	pub, err = ParsePublicKey(kf.PublicKey)
	if err != nil {
		return [32]byte{}, [32]byte{}, fmt.Errorf("parsing public key in %s: %w", name, err)
	}
	priv, err = ParsePrivateKey(kf.PrivateKey)
	if err != nil {
		return [32]byte{}, [32]byte{}, fmt.Errorf("parsing private key in %s: %w", name, err)
	}
	return pub, priv, nil
}

// LoadDefaultKeypair loads the user's default keypair from the platform-
// specific keys directory. Returns (publicKey, privateKey, error).
//
// Search order:
//  1. default.json in the keys directory
//  2. If no default, iterate named keypairs alphabetically, use first found.
//  3. If no keypairs exist, return ErrNoKeypair.
func LoadDefaultKeypair() (pub, priv [32]byte, err error) {
	return LoadDefaultKeypairFrom(DefaultKeysDir())
}

// LoadDefaultKeypairFrom loads the default keypair from a specific keys directory.
func LoadDefaultKeypairFrom(keysDir string) (pub, priv [32]byte, err error) {
	// Try default.json first.
	pub, priv, err = LoadKeypairFrom(keysDir, "default")
	if err == nil {
		return pub, priv, nil
	}

	// Iterate named keypairs alphabetically.
	entries, readErr := os.ReadDir(keysDir)
	if readErr != nil {
		return [32]byte{}, [32]byte{}, ErrNoKeypair
	}

	var names []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".json")
		if name == "default" {
			continue // already tried
		}
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		pub, priv, err = LoadKeypairFrom(keysDir, name)
		if err == nil {
			return pub, priv, nil
		}
	}

	return [32]byte{}, [32]byte{}, ErrNoKeypair
}

// LoadOrCreateKeypair loads the default keypair, or generates one if none exists.
// This is the primary entry point used by save/push flows.
//
// Returns (publicKey, privateKey, created, error) where created=true if a new
// keypair was generated.
func LoadOrCreateKeypair() (pub, priv [32]byte, created bool, err error) {
	pub, priv, err = LoadDefaultKeypair()
	if err == nil {
		return pub, priv, false, nil
	}
	if !errors.Is(err, ErrNoKeypair) {
		return [32]byte{}, [32]byte{}, false, err
	}
	// Auto-generate default keypair.
	pub, priv, err = GenerateKeypair()
	if err != nil {
		return [32]byte{}, [32]byte{}, false, err
	}
	if err := SaveKeypair("default", pub, priv); err != nil {
		return [32]byte{}, [32]byte{}, false, err
	}
	return pub, priv, true, nil
}

// ListKeypairs returns all keypair files in the keys directory.
func ListKeypairs() ([]KeypairFile, error) {
	return ListKeypairsFrom(DefaultKeysDir())
}

// ListKeypairsFrom returns all keypair files in a specific keys directory.
func ListKeypairsFrom(keysDir string) ([]KeypairFile, error) {
	entries, err := os.ReadDir(keysDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading keys directory: %w", err)
	}

	var result []KeypairFile
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		path := filepath.Join(keysDir, e.Name())
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			continue
		}
		var kf KeypairFile
		if json.Unmarshal(data, &kf) == nil {
			result = append(result, kf)
		}
	}

	sort.Slice(result, func(i, j int) bool {
		if result[i].Name == "default" {
			return true
		}
		if result[j].Name == "default" {
			return false
		}
		return result[i].Name < result[j].Name
	})

	return result, nil
}
