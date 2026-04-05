package registry

import (
	"fmt"
	"strings"
)

// ParseRef parses a checkpoint reference into a store name and tag.
//
// Accepted formats:
//
//	"myproject:cp-3"  -> storeName="myproject", tag="cp-3"
//	"myproject:latest" -> storeName="myproject", tag="latest"
//	"postgres-done"  -> storeName="", tag="postgres-done"
//	""               -> storeName="", tag="latest"
//
// When storeName is empty, callers should fall back to the current project name.
func ParseRef(ref string) (storeName string, tag string, err error) {
	ref = strings.TrimSpace(ref)

	if ref == "" {
		return "", "latest", nil
	}

	// Digest refs (sha256:abc...) are passed through as-is.
	// oras-go's Resolve handles both tags and digests.
	if strings.HasPrefix(ref, "sha256:") {
		return "", ref, nil
	}

	// Check for "name:tag" format.
	if idx := strings.LastIndex(ref, ":"); idx > 0 {
		storeName = ref[:idx]
		tag = ref[idx+1:]
		if storeName == "" || tag == "" {
			return "", "", fmt.Errorf("invalid ref %q: empty store name or tag", ref)
		}
		return storeName, tag, nil
	}

	// No colon found: treat the entire ref as a tag.
	// The caller is responsible for determining the store name
	// (typically the current project/directory name).
	return "", ref, nil
}

