package keys

import (
	"fmt"
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

	return [32]byte{}, fmt.Errorf("unknown recipient %q — add with: bento recipients add %s <bento-pk-...>", spec, spec)
}
