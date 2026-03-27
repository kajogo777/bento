package workspace

import (
	"testing"
)

func TestDiffLayersAddedFiles(t *testing.T) {
	oldFiles := map[string][]string{}
	newFiles := map[string][]string{
		"source": {"main.go", "util.go"},
	}

	result := DiffLayers(oldFiles, newFiles)

	r := result["source"]
	if r == nil {
		t.Fatal("expected source layer in result")
	}
	if len(r.Added) != 2 {
		t.Errorf("Added = %v, want 2 files", r.Added)
	}
	if len(r.Removed) != 0 {
		t.Errorf("Removed = %v, want empty", r.Removed)
	}
}

func TestDiffLayersRemovedFiles(t *testing.T) {
	oldFiles := map[string][]string{
		"source": {"main.go", "util.go", "old.go"},
	}
	newFiles := map[string][]string{
		"source": {"main.go"},
	}

	result := DiffLayers(oldFiles, newFiles)

	r := result["source"]
	if r == nil {
		t.Fatal("expected source layer in result")
	}
	if len(r.Removed) != 2 {
		t.Errorf("Removed = %v, want 2 files (util.go, old.go)", r.Removed)
	}
	if len(r.Modified) != 1 {
		t.Errorf("Modified = %v, want 1 file (main.go)", r.Modified)
	}
}

func TestDiffLayersUnchangedFiles(t *testing.T) {
	files := map[string][]string{
		"source": {"main.go", "util.go"},
		"deps":   {"go.mod"},
	}

	result := DiffLayers(files, files)

	for layer, r := range result {
		if len(r.Added) != 0 {
			t.Errorf("layer %q: Added = %v, want empty", layer, r.Added)
		}
		if len(r.Removed) != 0 {
			t.Errorf("layer %q: Removed = %v, want empty", layer, r.Removed)
		}
	}

	// All files should be in Modified (same layer in both).
	if r := result["source"]; r != nil {
		if len(r.Modified) != 2 {
			t.Errorf("source Modified = %v, want 2 files", r.Modified)
		}
	}
	if r := result["deps"]; r != nil {
		if len(r.Modified) != 1 {
			t.Errorf("deps Modified = %v, want 1 file", r.Modified)
		}
	}
}

func TestDiffLayersFileMovedBetweenLayers(t *testing.T) {
	oldFiles := map[string][]string{
		"source": {"main.go", "config.yml"},
	}
	newFiles := map[string][]string{
		"source": {"main.go"},
		"config": {"config.yml"},
	}

	result := DiffLayers(oldFiles, newFiles)

	// config.yml should be Added in config layer and Removed from source layer.
	if r := result["config"]; r == nil {
		t.Fatal("expected config layer in result")
	} else {
		if len(r.Added) != 1 || r.Added[0] != "config.yml" {
			t.Errorf("config Added = %v, want [config.yml]", r.Added)
		}
	}

	if r := result["source"]; r == nil {
		t.Fatal("expected source layer in result")
	} else {
		found := false
		for _, f := range r.Removed {
			if f == "config.yml" {
				found = true
			}
		}
		if !found {
			t.Errorf("source Removed = %v, want config.yml in list", r.Removed)
		}
	}
}

func TestDiffLayersBothEmpty(t *testing.T) {
	result := DiffLayers(map[string][]string{}, map[string][]string{})
	if len(result) != 0 {
		t.Errorf("expected empty result for two empty maps, got %d layers", len(result))
	}
}

func TestDiffLayersMixedChanges(t *testing.T) {
	oldFiles := map[string][]string{
		"source": {"main.go", "removed.go"},
		"deps":   {"go.mod"},
	}
	newFiles := map[string][]string{
		"source": {"main.go", "new.go"},
		"deps":   {"go.mod", "go.sum"},
	}

	result := DiffLayers(oldFiles, newFiles)

	src := result["source"]
	if src == nil {
		t.Fatal("expected source layer")
	}
	if len(src.Added) != 1 || src.Added[0] != "new.go" {
		t.Errorf("source Added = %v, want [new.go]", src.Added)
	}
	if len(src.Removed) != 1 || src.Removed[0] != "removed.go" {
		t.Errorf("source Removed = %v, want [removed.go]", src.Removed)
	}
	if len(src.Modified) != 1 || src.Modified[0] != "main.go" {
		t.Errorf("source Modified = %v, want [main.go]", src.Modified)
	}

	deps := result["deps"]
	if deps == nil {
		t.Fatal("expected deps layer")
	}
	if len(deps.Added) != 1 || deps.Added[0] != "go.sum" {
		t.Errorf("deps Added = %v, want [go.sum]", deps.Added)
	}
	if len(deps.Modified) != 1 || deps.Modified[0] != "go.mod" {
		t.Errorf("deps Modified = %v, want [go.mod]", deps.Modified)
	}
}
