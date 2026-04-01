package secrets

import (
	"strings"
	"testing"
)

func TestScrubFile_NoFindings(t *testing.T) {
	content := []byte(`{"token": "__BENTO_SCRUBBED[f2cf17649421]__"}`)
	scrubbed, replacements := ScrubFile(content, nil)

	if string(scrubbed) != string(content) {
		t.Errorf("expected unchanged content, got %q", scrubbed)
	}
	if replacements != nil {
		t.Errorf("expected nil replacements, got %v", replacements)
	}
}

func TestScrubFile_SingleFinding(t *testing.T) {
	content := []byte(`{"token": "__BENTO_SCRUBBED[f2cf17649421]__"}`)
	findings := []ScanResult{
		{Match: "__BENTO_SCRUBBED[f2cf17649421]__", Pattern: "openai-api-key"},
	}

	scrubbed, replacements := ScrubFile(content, findings)

	if len(replacements) != 1 {
		t.Fatalf("expected 1 replacement, got %d", len(replacements))
	}

	r := replacements[0]
	if r.RuleID != "openai-api-key" {
		t.Errorf("expected ruleID openai-api-key, got %q", r.RuleID)
	}
	if r.Secret() != "__BENTO_SCRUBBED[f2cf17649421]__" {
		t.Errorf("expected secret __BENTO_SCRUBBED[f2cf17649421]__, got %q", r.Secret())
	}
	if !strings.Contains(r.Placeholder, "__BENTO_SCRUBBED[") {
		t.Errorf("placeholder doesn't match format: %q", r.Placeholder)
	}

	// Original secret must not appear in scrubbed content.
	if strings.Contains(string(scrubbed), "__BENTO_SCRUBBED[f2cf17649421]__") {
		t.Error("scrubbed content still contains the secret")
	}
	// Placeholder must appear.
	if !strings.Contains(string(scrubbed), r.Placeholder) {
		t.Error("scrubbed content doesn't contain the placeholder")
	}
	// Original content must not be modified.
	if !strings.Contains(string(content), "__BENTO_SCRUBBED[f2cf17649421]__") {
		t.Error("original content was modified")
	}
}

func TestScrubFile_MultipleFindings(t *testing.T) {
	content := []byte(`{
  "openai": "__BENTO_SCRUBBED[f2cf17649421]__",
  "github": "ghp_xyz789token"
}`)
	findings := []ScanResult{
		{Match: "__BENTO_SCRUBBED[f2cf17649421]__", Pattern: "openai-api-key"},
		{Match: "ghp_xyz789token", Pattern: "github-token"},
	}

	scrubbed, replacements := ScrubFile(content, findings)

	if len(replacements) != 2 {
		t.Fatalf("expected 2 replacements, got %d", len(replacements))
	}

	// Both secrets must be gone.
	s := string(scrubbed)
	if strings.Contains(s, "__BENTO_SCRUBBED[f2cf17649421]__") {
		t.Error("scrubbed content still contains openai secret")
	}
	if strings.Contains(s, "ghp_xyz789token") {
		t.Error("scrubbed content still contains github secret")
	}

	// Placeholders must be unique.
	if replacements[0].Placeholder == replacements[1].Placeholder {
		t.Error("placeholders are not unique")
	}
}

func TestScrubFile_DuplicateSecret(t *testing.T) {
	// Same secret appears twice in the file.
	content := []byte(`{
  "primary": "__BENTO_SCRUBBED[f2cf17649421]__",
  "fallback": "__BENTO_SCRUBBED[f2cf17649421]__"
}`)
	findings := []ScanResult{
		{Match: "__BENTO_SCRUBBED[f2cf17649421]__", Pattern: "openai-api-key"},
		{Match: "__BENTO_SCRUBBED[f2cf17649421]__", Pattern: "openai-api-key"},
	}

	scrubbed, replacements := ScrubFile(content, findings)

	// Deduplicated: one replacement for both occurrences.
	if len(replacements) != 1 {
		t.Fatalf("expected 1 replacement (deduped), got %d", len(replacements))
	}

	// Both occurrences must be replaced.
	count := strings.Count(string(scrubbed), replacements[0].Placeholder)
	if count != 2 {
		t.Errorf("expected placeholder to appear 2 times, got %d", count)
	}
}

func TestScrubFile_EmptyMatch(t *testing.T) {
	content := []byte(`{"token": "__BENTO_SCRUBBED[f2cf17649421]__"}`)
	findings := []ScanResult{
		{Match: "", Pattern: "empty-rule"},
	}

	scrubbed, replacements := ScrubFile(content, findings)

	if len(replacements) != 0 {
		t.Errorf("expected 0 replacements for empty match, got %d", len(replacements))
	}
	if string(scrubbed) != string(content) {
		t.Error("content should be unchanged for empty match")
	}
}

func TestHydrateFile_Basic(t *testing.T) {
	placeholder := "__BENTO_SCRUBBED[aabbccddeeff]__"
	content := []byte(`{"token": "` + placeholder + `"}`)
	values := map[string]string{
		placeholder: "__BENTO_SCRUBBED[f2cf17649421]__",
	}

	hydrated := HydrateFile(content, values)

	if !strings.Contains(string(hydrated), "__BENTO_SCRUBBED[f2cf17649421]__") {
		t.Error("hydrated content doesn't contain the secret")
	}
	if strings.Contains(string(hydrated), placeholder) {
		t.Error("hydrated content still contains the placeholder")
	}
}

func TestHydrateFile_NoValues(t *testing.T) {
	content := []byte(`{"token": "__BENTO_SCRUBBED[aabbccddeeff]__"}`)
	hydrated := HydrateFile(content, nil)

	if string(hydrated) != string(content) {
		t.Error("content should be unchanged with nil values")
	}
}

func TestHydrateFile_UnknownPlaceholder(t *testing.T) {
	content := []byte(`{"a": "__BENTO_SCRUBBED[aabbccddeeff]__", "b": "__BENTO_SCRUBBED[112233445566]__"}`)
	values := map[string]string{
		"__BENTO_SCRUBBED[aabbccddeeff]__": "secret-a",
		// 112233445566 not in values — should be left as-is.
	}

	hydrated := HydrateFile(content, values)

	if !strings.Contains(string(hydrated), "secret-a") {
		t.Error("known placeholder not hydrated")
	}
	if !strings.Contains(string(hydrated), "__BENTO_SCRUBBED[112233445566]__") {
		t.Error("unknown placeholder should be left as-is")
	}
}

func TestScrubHydrate_RoundTrip(t *testing.T) {
	original := []byte(`{
  "mcpServers": {
    "api": {
      "env": {
        "OPENAI_API_KEY": "__BENTO_SCRUBBED[f2cf17649421]__def456",
        "GITHUB_TOKEN": "ghp_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"
      }
    }
  }
}`)
	findings := []ScanResult{
		{Match: "__BENTO_SCRUBBED[f2cf17649421]__def456", Pattern: "openai-api-key"},
		{Match: "ghp_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx", Pattern: "github-token"},
	}

	// Scrub
	scrubbed, replacements := ScrubFile(original, findings)

	// Build values map (simulates backend round-trip)
	values := make(map[string]string)
	for _, r := range replacements {
		values[r.Placeholder] = r.Secret()
	}

	// Hydrate
	hydrated := HydrateFile(scrubbed, values)

	if string(hydrated) != string(original) {
		t.Errorf("round-trip failed.\nOriginal:\n%s\nHydrated:\n%s", original, hydrated)
	}
}

func TestPlaceholderFormat(t *testing.T) {
	content := []byte("test content")
	findings := []ScanResult{
		{Match: "test content", Pattern: "test-rule"},
	}

	_, replacements := ScrubFile(content, findings)
	if len(replacements) != 1 {
		t.Fatal("expected 1 replacement")
	}

	ph := replacements[0].Placeholder
	if !placeholderRe.MatchString(ph) {
		t.Errorf("placeholder %q doesn't match expected format __BENTO_SCRUBBED[12hex]__", ph)
	}
}
