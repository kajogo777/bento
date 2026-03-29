package workspace

import (
	"fmt"
	"strings"
)

// UnifiedDiff produces a unified diff string between oldLines and newLines,
// using the standard ---/+++/@@ format with contextLines lines of context.
// Returns an empty string if there are no changes.
func UnifiedDiff(oldName, newName string, oldLines, newLines []string, contextLines int) string {
	if contextLines < 0 {
		contextLines = 3
	}

	// Compute edit script via LCS
	script := computeScript(oldLines, newLines)
	if len(script) == 0 {
		return ""
	}

	// Build hunks from the script
	hunks := buildHunks(script, contextLines)
	if len(hunks) == 0 {
		return ""
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "--- %s\n", oldName)
	fmt.Fprintf(&sb, "+++ %s\n", newName)

	for _, h := range hunks {
		fmt.Fprintf(&sb, "@@ -%d,%d +%d,%d @@\n", h.oldStart, h.oldCount, h.newStart, h.newCount)
		for _, l := range h.lines {
			sb.WriteString(l)
			sb.WriteByte('\n')
		}
	}

	return sb.String()
}

// scriptEntry represents one line in the diff script.
type scriptEntry struct {
	op   byte   // ' ' equal, '-' delete, '+' insert
	text string // line content
	oldI int    // 0-based index in old (-1 if insert)
	newI int    // 0-based index in new (-1 if delete)
}

// computeScript builds an ordered edit script from old to new using LCS DP.
func computeScript(old, new []string) []scriptEntry {
	n, m := len(old), len(new)

	// DP table for LCS lengths
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if old[i] == new[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}

	// Backtrack
	var script []scriptEntry
	hasChanges := false
	i, j := 0, 0
	for i < n && j < m {
		if old[i] == new[j] {
			script = append(script, scriptEntry{op: ' ', text: old[i], oldI: i, newI: j})
			i++
			j++
		} else if dp[i+1][j] >= dp[i][j+1] {
			script = append(script, scriptEntry{op: '-', text: old[i], oldI: i, newI: -1})
			hasChanges = true
			i++
		} else {
			script = append(script, scriptEntry{op: '+', text: new[j], oldI: -1, newI: j})
			hasChanges = true
			j++
		}
	}
	for i < n {
		script = append(script, scriptEntry{op: '-', text: old[i], oldI: i, newI: -1})
		hasChanges = true
		i++
	}
	for j < m {
		script = append(script, scriptEntry{op: '+', text: new[j], oldI: -1, newI: j})
		hasChanges = true
		j++
	}

	if !hasChanges {
		return nil
	}
	return script
}

type hunk struct {
	oldStart int // 1-based
	oldCount int
	newStart int // 1-based
	newCount int
	lines    []string
}

// buildHunks groups script entries into hunks with ctx lines of context,
// merging hunks that overlap or are adjacent.
func buildHunks(script []scriptEntry, ctx int) []hunk {
	// Find indices of changed lines
	var changeIdx []int
	for i, e := range script {
		if e.op != ' ' {
			changeIdx = append(changeIdx, i)
		}
	}
	if len(changeIdx) == 0 {
		return nil
	}

	// Group changes into ranges, merging when context overlaps
	type span struct{ lo, hi int } // inclusive indices into script
	var spans []span

	lo := changeIdx[0] - ctx
	if lo < 0 {
		lo = 0
	}
	hi := changeIdx[0] + ctx
	if hi >= len(script) {
		hi = len(script) - 1
	}

	for _, ci := range changeIdx[1:] {
		newLo := ci - ctx
		if newLo < 0 {
			newLo = 0
		}
		newHi := ci + ctx
		if newHi >= len(script) {
			newHi = len(script) - 1
		}

		if newLo <= hi+1 {
			// Merge
			hi = newHi
		} else {
			spans = append(spans, span{lo, hi})
			lo = newLo
			hi = newHi
		}
	}
	spans = append(spans, span{lo, hi})

	// Convert spans to hunks
	var hunks []hunk
	for _, s := range spans {
		var h hunk
		oldCount, newCount := 0, 0

		// Determine 1-based start positions
		oldStart := -1
		newStart := -1
		for i := s.lo; i <= s.hi; i++ {
			e := script[i]
			if oldStart == -1 && e.oldI >= 0 {
				oldStart = e.oldI + 1
			}
			if newStart == -1 && e.newI >= 0 {
				newStart = e.newI + 1
			}
			if oldStart != -1 && newStart != -1 {
				break
			}
		}
		// Handle edge: no old lines in hunk (all inserts) or no new lines (all deletes)
		if oldStart == -1 {
			oldStart = 0
		}
		if newStart == -1 {
			newStart = 0
		}

		for i := s.lo; i <= s.hi; i++ {
			e := script[i]
			h.lines = append(h.lines, string(e.op)+e.text)
			switch e.op {
			case ' ':
				oldCount++
				newCount++
			case '-':
				oldCount++
			case '+':
				newCount++
			}
		}

		h.oldStart = oldStart
		h.newStart = newStart
		h.oldCount = oldCount
		h.newCount = newCount
		hunks = append(hunks, h)
	}

	return hunks
}
