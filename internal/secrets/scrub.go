package secrets

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"regexp"
	"sort"
)

// Replacement records a single secret scrub within a file.
type Replacement struct {
	// Placeholder is the unique string that replaced the secret value
	// (e.g., "__BENTO_SCRUBBED[a1b2c3d4e5f6]__").
	Placeholder string

	// RuleID is the gitleaks rule that detected the secret (e.g., "openai-api-key").
	// Used for diagnostics and display only.
	RuleID string

	// secret holds the original secret value. Exported only within this
	// package for backend storage; never serialized to OCI manifests.
	secret string
}

// Secret returns the original secret value. Used by the save flow to
// build the placeholder→value map for the backend.
func (r Replacement) Secret() string { return r.secret }

// ScrubFileRecord groups all replacements for a single file.
type ScrubFileRecord struct {
	Path         string        // relative file path
	Replacements []Replacement // all scrubs in this file
}

// placeholderRe matches bento scrub placeholders in file content.
var placeholderRe = regexp.MustCompile(`__BENTO_SCRUBBED\[[0-9a-f]{12}\]__`)

// PlaceholderRe returns the compiled regex for matching scrub placeholders.
func PlaceholderRe() *regexp.Regexp { return placeholderRe }

// ScrubFile takes file content and gitleaks scan results for that file.
// It replaces each detected secret value with a unique placeholder and
// returns the scrubbed content plus the list of replacements made.
//
// The original content slice is not modified; a new byte slice is returned.
// If findings is empty, the original content is returned unchanged with
// a nil replacements slice.
func ScrubFile(content []byte, findings []ScanResult) (scrubbed []byte, replacements []Replacement) {
	if len(findings) == 0 {
		return content, nil
	}

	result := make([]byte, len(content))
	copy(result, content)

	// Deduplicate findings by secret value — the same token may appear
	// multiple times in a file (e.g., used in two config blocks). Each
	// unique secret value gets one placeholder, and all occurrences are
	// replaced.
	type deduped struct {
		secret      string
		ruleID      string
		placeholder string
	}
	seen := make(map[string]*deduped)
	var ordered []*deduped

	for _, f := range findings {
		if f.Match == "" {
			continue
		}
		if _, ok := seen[f.Match]; !ok {
			ph := generatePlaceholder(result)
			d := &deduped{
				secret:      f.Match,
				ruleID:      f.Pattern,
				placeholder: ph,
			}
			seen[f.Match] = d
			ordered = append(ordered, d)
		}
	}

	// Sort by secret length descending so that longer secrets are replaced
	// before shorter ones that may be substrings. Without this, replacing
	// "key1" before "key1234" would corrupt the longer secret.
	sort.Slice(ordered, func(i, j int) bool {
		return len(ordered[i].secret) > len(ordered[j].secret)
	})

	for _, d := range ordered {
		result = bytes.ReplaceAll(result, []byte(d.secret), []byte(d.placeholder))
		replacements = append(replacements, Replacement{
			Placeholder: d.placeholder,
			RuleID:      d.ruleID,
			secret:      d.secret,
		})
	}

	return result, replacements
}

// HydrateFile takes scrubbed file content and a map of placeholder→value.
// It replaces all known placeholders with their real values and returns
// the hydrated content. Unknown placeholders are left as-is.
func HydrateFile(content []byte, values map[string]string) []byte {
	if len(values) == 0 {
		return content
	}

	result := make([]byte, len(content))
	copy(result, content)

	for placeholder, value := range values {
		result = bytes.ReplaceAll(result, []byte(placeholder), []byte(value))
	}

	return result
}

// generatePlaceholder creates a unique __BENTO_SCRUBBED[<12hex>]__ string
// that does not already appear in the given content.
func generatePlaceholder(content []byte) string {
	for {
		b := make([]byte, 6) // 6 bytes = 12 hex chars
		if _, err := rand.Read(b); err != nil {
			panic(fmt.Sprintf("crypto/rand failed: %v", err))
		}
		id := hex.EncodeToString(b)
		ph := fmt.Sprintf("__BENTO_SCRUBBED[%s]__", id)
		if !bytes.Contains(content, []byte(ph)) {
			return ph
		}
	}
}
