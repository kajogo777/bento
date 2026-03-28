package workspace

import (
	"strings"
	"testing"
)

func TestUnifiedDiffNoChanges(t *testing.T) {
	lines := []string{"a", "b", "c"}
	diff := UnifiedDiff("old", "new", lines, lines, 3)
	if diff != "" {
		t.Errorf("expected empty diff for identical input, got:\n%s", diff)
	}
}

func TestUnifiedDiffAddedFile(t *testing.T) {
	diff := UnifiedDiff("a/file", "b/file", nil, []string{"hello", "world"}, 3)
	if !strings.Contains(diff, "+hello") {
		t.Errorf("expected +hello in diff:\n%s", diff)
	}
	if !strings.Contains(diff, "+world") {
		t.Errorf("expected +world in diff:\n%s", diff)
	}
}

func TestUnifiedDiffRemovedFile(t *testing.T) {
	diff := UnifiedDiff("a/file", "b/file", []string{"hello", "world"}, nil, 3)
	if !strings.Contains(diff, "-hello") {
		t.Errorf("expected -hello in diff:\n%s", diff)
	}
}

func TestUnifiedDiffModified(t *testing.T) {
	old := []string{"line1", "line2", "line3"}
	new := []string{"line1", "changed", "line3"}
	diff := UnifiedDiff("a/file", "b/file", old, new, 3)

	if !strings.Contains(diff, "--- a/file") {
		t.Errorf("expected --- header in diff:\n%s", diff)
	}
	if !strings.Contains(diff, "+++ b/file") {
		t.Errorf("expected +++ header in diff:\n%s", diff)
	}
	if !strings.Contains(diff, "-line2") {
		t.Errorf("expected -line2 in diff:\n%s", diff)
	}
	if !strings.Contains(diff, "+changed") {
		t.Errorf("expected +changed in diff:\n%s", diff)
	}
	if !strings.Contains(diff, " line1") {
		t.Errorf("expected context line ' line1' in diff:\n%s", diff)
	}
}

func TestUnifiedDiffHunkHeader(t *testing.T) {
	old := []string{"a", "b", "c"}
	new := []string{"a", "x", "c"}
	diff := UnifiedDiff("old", "new", old, new, 3)

	if !strings.Contains(diff, "@@") {
		t.Errorf("expected @@ hunk header in diff:\n%s", diff)
	}
}

func TestUnifiedDiffMultipleHunks(t *testing.T) {
	// Create files with changes far apart to get separate hunks
	old := make([]string, 20)
	new := make([]string, 20)
	for i := range old {
		old[i] = strings.Repeat("x", i+1)
		new[i] = old[i]
	}
	old[1] = "OLD_FIRST"
	new[1] = "NEW_FIRST"
	old[18] = "OLD_LAST"
	new[18] = "NEW_LAST"

	diff := UnifiedDiff("old", "new", old, new, 2)

	// Should have two @@ headers since changes are far apart with ctx=2
	count := strings.Count(diff, "@@")
	if count < 4 { // 2 hunks × 2 @@ per header line
		t.Errorf("expected at least 2 hunk headers (4 @@), got %d @@ occurrences in:\n%s", count, diff)
	}
}
