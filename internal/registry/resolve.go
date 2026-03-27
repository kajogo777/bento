package registry

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ParseRef parses a checkpoint reference into a store name and tag.
//
// Accepted formats:
//
//	"myproject:cp-3"  -> storeName="myproject", tag="cp-3"
//	"cp-3"           -> storeName=<current directory name>, tag="cp-3"
//	"myproject"      -> storeName="myproject", tag="latest"
//	""               -> storeName=<current directory name>, tag="latest"
func ParseRef(ref string) (storeName string, tag string, err error) {
	ref = strings.TrimSpace(ref)

	if ref == "" {
		storeName, err = currentDirName()
		if err != nil {
			return "", "", err
		}
		return storeName, "latest", nil
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

	// If the ref looks like a tag (contains "cp-" or is a known tag pattern),
	// treat it as a tag with the current directory as the store name.
	// Otherwise treat it as a store name with "latest" tag.
	if looksLikeTag(ref) {
		storeName, err = currentDirName()
		if err != nil {
			return "", "", err
		}
		return storeName, ref, nil
	}

	return ref, "latest", nil
}

// looksLikeTag returns true if the string looks like a tag rather than a project name.
func looksLikeTag(s string) bool {
	// Tags typically start with "cp-" or "checkpoint-" or are "latest".
	if s == "latest" {
		return true
	}
	if strings.HasPrefix(s, "cp-") || strings.HasPrefix(s, "checkpoint-") {
		return true
	}
	return false
}

// currentDirName returns the base name of the current working directory.
func currentDirName() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get working directory: %w", err)
	}
	return filepath.Base(wd), nil
}
