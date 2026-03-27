package workspace

import (
	"bufio"
	"os"
	"path"
	"strings"
)

// IgnoreMatcher holds compiled ignore patterns and matches file paths against them.
type IgnoreMatcher struct {
	patterns []string
}

// NewIgnoreMatcher creates a new IgnoreMatcher from the given patterns.
func NewIgnoreMatcher(patterns []string) *IgnoreMatcher {
	return &IgnoreMatcher{patterns: patterns}
}

// Match returns true if the path should be ignored.
func (m *IgnoreMatcher) Match(p string) bool {
	p = NormalizePath(p)
	for _, pattern := range m.patterns {
		if matchesPattern(pattern, p) {
			return true
		}
	}
	return false
}

// LoadBentoIgnore loads a .bentoignore file from dir and returns the patterns
// found within it. Each non-empty, non-comment line is treated as a pattern.
func LoadBentoIgnore(dir string) ([]string, error) {
	fp := path.Join(dir, ".bentoignore")
	f, err := os.Open(fp)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var patterns []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		patterns = append(patterns, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return patterns, nil
}

// matchesPattern checks whether path matches a glob pattern, supporting **
// globstar syntax. A pattern ending with "/**" matches any file under that
// directory. A pattern containing "**/" matches any leading directory prefix.
func matchesPattern(pattern, p string) bool {
	pattern = NormalizePath(pattern)

	// Handle ** globstar patterns.
	if strings.Contains(pattern, "**") {
		// Pattern like "foo/**" matches anything under foo/.
		if strings.HasSuffix(pattern, "/**") {
			prefix := strings.TrimSuffix(pattern, "/**")
			if p == prefix || strings.HasPrefix(p, prefix+"/") {
				return true
			}
			return false
		}
		// Pattern like "**/bar" matches bar in any directory.
		if strings.HasPrefix(pattern, "**/") {
			suffix := strings.TrimPrefix(pattern, "**/")
			if p == suffix || strings.HasSuffix(p, "/"+suffix) {
				return true
			}
			// Also try matching each path segment.
			parts := strings.Split(p, "/")
			for i := range parts {
				sub := strings.Join(parts[i:], "/")
				matched, _ := path.Match(suffix, sub)
				if matched {
					return true
				}
			}
			return false
		}
		// General ** in the middle: split on ** and match segments.
		parts := strings.SplitN(pattern, "**", 2)
		prefix := parts[0]
		suffix := parts[1]
		if prefix != "" && !strings.HasPrefix(p, prefix) {
			return false
		}
		rest := strings.TrimPrefix(p, prefix)
		// The suffix may start with "/".
		suffix = strings.TrimPrefix(suffix, "/")
		if suffix == "" {
			return true
		}
		// Try matching the suffix against every possible tail of the remaining path.
		segments := strings.Split(rest, "/")
		for i := range segments {
			tail := strings.Join(segments[i:], "/")
			matched, _ := path.Match(suffix, tail)
			if matched {
				return true
			}
		}
		return false
	}

	// Simple glob pattern using path.Match.
	matched, _ := path.Match(pattern, p)
	if matched {
		return true
	}

	// Also try matching against just the base name for patterns without slashes.
	if !strings.Contains(pattern, "/") {
		matched, _ = path.Match(pattern, path.Base(p))
		if matched {
			return true
		}
	}

	return false
}
