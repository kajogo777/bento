package workspace

import (
	"sort"
)

// DiffResult holds the per-layer diff between two file sets.
type DiffResult struct {
	Added    []string
	Modified []string
	Removed  []string
}

// DiffLayers compares two layer-to-files maps (old and new) and returns a
// per-layer DiffResult. A file present only in newFiles is Added; a file
// present only in oldFiles is Removed; a file present in both but assigned to
// a different layer is treated as Added in the new layer and Removed from the
// old layer. Files present in the same layer in both maps are listed as Modified
// (since content may have changed).
func DiffLayers(oldFiles, newFiles map[string][]string) map[string]*DiffResult {
	results := make(map[string]*DiffResult)

	ensureResult := func(layer string) *DiffResult {
		if results[layer] == nil {
			results[layer] = &DiffResult{}
		}
		return results[layer]
	}

	// Build lookup: file -> layer for old and new.
	oldLookup := make(map[string]string)
	for layer, files := range oldFiles {
		for _, f := range files {
			oldLookup[f] = layer
		}
	}

	newLookup := make(map[string]string)
	for layer, files := range newFiles {
		for _, f := range files {
			newLookup[f] = layer
		}
	}

	// Find added and modified files.
	for file, newLayer := range newLookup {
		r := ensureResult(newLayer)
		oldLayer, existed := oldLookup[file]
		if !existed {
			r.Added = append(r.Added, file)
		} else if oldLayer == newLayer {
			r.Modified = append(r.Modified, file)
		} else {
			// File moved layers: added in new, removed from old.
			r.Added = append(r.Added, file)
			oldR := ensureResult(oldLayer)
			oldR.Removed = append(oldR.Removed, file)
		}
	}

	// Find removed files (in old but not in new).
	for file, oldLayer := range oldLookup {
		if _, exists := newLookup[file]; !exists {
			r := ensureResult(oldLayer)
			r.Removed = append(r.Removed, file)
		}
	}

	// Sort all slices for deterministic output.
	for _, r := range results {
		sort.Strings(r.Added)
		sort.Strings(r.Modified)
		sort.Strings(r.Removed)
	}

	return results
}
