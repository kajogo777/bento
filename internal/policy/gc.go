package policy

import (
	"sort"
	"time"

	"github.com/bentoci/bento/internal/registry"
)

// GCOptions configures garbage collection behavior.
type GCOptions struct {
	KeepLast   int
	KeepTagged bool
}

// GarbageCollect removes old checkpoints from the store according to the given
// options. It keeps the most recent KeepLast checkpoints and, if KeepTagged is
// true, any checkpoint that has a tag. It returns the list of deleted digests.
func GarbageCollect(store registry.Store, opts GCOptions) (deleted []string, err error) {
	entries, err := store.ListCheckpoints()
	if err != nil {
		return nil, err
	}

	// Sort by creation time, newest first.
	sort.Slice(entries, func(i, j int) bool {
		ti, _ := time.Parse(time.RFC3339, entries[i].Created)
		tj, _ := time.Parse(time.RFC3339, entries[j].Created)
		return ti.After(tj)
	})

	keep := make(map[string]bool)

	// Keep the last N checkpoints.
	for i := 0; i < len(entries) && i < opts.KeepLast; i++ {
		keep[entries[i].Digest] = true
	}

	// Keep tagged checkpoints if requested.
	if opts.KeepTagged {
		for _, e := range entries {
			if e.Tag != "" {
				keep[e.Digest] = true
			}
		}
	}

	// Delete everything not marked to keep.
	for _, e := range entries {
		if keep[e.Digest] {
			continue
		}
		if err := store.DeleteCheckpoint(e.Digest); err != nil {
			return deleted, err
		}
		deleted = append(deleted, e.Digest)
	}

	return deleted, nil
}
