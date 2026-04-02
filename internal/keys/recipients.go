package keys

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ConfigRecipient represents a recipient entry from bento.yaml.
type ConfigRecipient struct {
	Name string `yaml:"name"`
	Key  string `yaml:"key"`
}

// ResolveRecipients resolves a list of recipient specifiers to public keys.
//
// Each specifier is resolved in order:
//  1. If it starts with "bento-pk-": parse as a literal public key
//  2. If it matches a name in bento.yaml recipients: use that key
//  3. If it matches a file in keysDir/recipients/<name>.pub: read that file
//  4. Otherwise: return an error with actionable message
//
// The sender's own public key is always appended (implicit self-recipient)
// and the list is deduplicated by public key bytes.
func ResolveRecipients(
	specifiers []string,
	configRecipients []ConfigRecipient,
	senderPub [32]byte,
	keysDir string,
) ([][32]byte, error) {
	if keysDir == "" {
		keysDir = DefaultKeysDir()
	}

	var result [][32]byte
	seen := make(map[[32]byte]bool)

	for _, spec := range specifiers {
		pk, err := resolveOne(spec, configRecipients, keysDir)
		if err != nil {
			return nil, err
		}
		if !seen[pk] {
			seen[pk] = true
			result = append(result, pk)
		}
	}

	// Always include sender as implicit recipient.
	if !seen[senderPub] {
		result = append(result, senderPub)
	}

	return result, nil
}

// resolveOne resolves a single recipient specifier to a public key.
func resolveOne(spec string, configRecipients []ConfigRecipient, keysDir string) ([32]byte, error) {
	// 1. Literal public key.
	if strings.HasPrefix(spec, PrefixPublicKey) {
		pk, err := ParsePublicKey(spec)
		if err != nil {
			return [32]byte{}, fmt.Errorf("invalid public key: %w", err)
		}
		return pk, nil
	}

	// 2. Check bento.yaml recipients by name.
	for _, cr := range configRecipients {
		if cr.Name == spec {
			pk, err := ParsePublicKey(cr.Key)
			if err != nil {
				return [32]byte{}, fmt.Errorf("invalid public key for recipient %q in bento.yaml: %w", spec, err)
			}
			return pk, nil
		}
	}

	// 3. Check recipients directory.
	pk, err := LoadRecipientFile(keysDir, spec)
	if err == nil {
		return pk, nil
	}

	return [32]byte{}, fmt.Errorf("unknown recipient %q — add with: bento recipients add %s <bento-pk-...>", spec, spec)
}

// LoadRecipientFile reads a recipient's public key from keysDir/recipients/<name>.pub.
func LoadRecipientFile(keysDir, name string) ([32]byte, error) {
	path := filepath.Join(keysDir, "recipients", name+".pub")
	data, err := os.ReadFile(path)
	if err != nil {
		return [32]byte{}, err
	}
	return parseRecipientFileContent(data)
}

// parseRecipientFileContent parses the content of a .pub file.
// The first non-comment, non-blank line is used as the public key.
func parseRecipientFileContent(data []byte) ([32]byte, error) {
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		return ParsePublicKey(line)
	}
	return [32]byte{}, fmt.Errorf("recipient file is empty or contains only comments")
}

// AddRecipient saves a recipient's public key to keysDir/recipients/<name>.pub.
func AddRecipient(name, publicKey string) error {
	return AddRecipientTo(DefaultKeysDir(), name, publicKey)
}

// AddRecipientTo saves a recipient's public key to a specific keysDir.
func AddRecipientTo(keysDir, name, publicKey string) error {
	// Validate the key before saving.
	if _, err := ParsePublicKey(publicKey); err != nil {
		return err
	}

	dir := filepath.Join(keysDir, "recipients")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("creating recipients directory: %w", err)
	}

	content := fmt.Sprintf("# %s's bento public key\n%s\n", name, publicKey)
	path := filepath.Join(dir, name+".pub")
	return os.WriteFile(path, []byte(content), 0644)
}

// RemoveRecipient removes a recipient's .pub file.
func RemoveRecipient(name string) error {
	return RemoveRecipientFrom(DefaultKeysDir(), name)
}

// RemoveRecipientFrom removes a recipient's .pub file from a specific keysDir.
func RemoveRecipientFrom(keysDir, name string) error {
	path := filepath.Join(keysDir, "recipients", name+".pub")
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return fmt.Errorf("recipient %q not found", name)
	}
	if err != nil {
		return fmt.Errorf("removing recipient %q: %w", name, err)
	}
	return nil
}

// ListRecipientFiles returns all recipient files from the keys directory,
// along with their source (file path).
type RecipientInfo struct {
	Name      string
	PublicKey string
	Source    string // "bento.yaml" or file path
}

// ListRecipients lists all known recipients from both bento.yaml and the
// recipients directory.
func ListRecipients(configRecipients []ConfigRecipient, keysDir string) []RecipientInfo {
	if keysDir == "" {
		keysDir = DefaultKeysDir()
	}
	var result []RecipientInfo

	// From bento.yaml.
	for _, cr := range configRecipients {
		result = append(result, RecipientInfo{
			Name:      cr.Name,
			PublicKey: cr.Key,
			Source:    "bento.yaml",
		})
	}

	// From recipients directory.
	recipDir := filepath.Join(keysDir, "recipients")
	entries, err := os.ReadDir(recipDir)
	if err != nil {
		return result
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".pub") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".pub")
		data, readErr := os.ReadFile(filepath.Join(recipDir, e.Name()))
		if readErr != nil {
			continue
		}
		pk, parseErr := parseRecipientFileContent(data)
		if parseErr != nil {
			continue
		}
		result = append(result, RecipientInfo{
			Name:      name,
			PublicKey: FormatPublicKey(pk),
			Source:    filepath.Join(keysDir, "recipients", e.Name()),
		})
	}

	return result
}
