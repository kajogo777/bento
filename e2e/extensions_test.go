//go:build integration

package e2e_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// TestClaudeCodeExtension: .claude/ and CLAUDE.md land in the agent layer
// ---------------------------------------------------------------------------

func TestClaudeCodeExtension(t *testing.T) {
	dir := makeWorkspace(t)

	// Seed Claude Code marker files.
	writeFile(t, dir, "CLAUDE.md", "# Claude Instructions\nUse Go.\n")
	writeFile(t, dir, ".claude/settings.json", `{"model":"claude-sonnet-4-20250514"}`)
	writeFile(t, dir, ".claude/rules/testing.md", "# Testing rules\nAlways write tests.\n")
	writeFile(t, dir, ".claude/settings.local.json", `{"localOnly":true}`)

	out := run(t, dir, "save", "--skip-secret-scan", "-m", "claude-code test")

	if !strings.Contains(out, "claude-code") {
		t.Logf("save output: %s", out)
	}

	// Inspect with --files to verify agent layer contents.
	inspectOut := run(t, dir, "inspect", "--files")

	// Agent layer should contain Claude Code files.
	for _, expected := range []string{"CLAUDE.md", ".claude/settings.json", ".claude/rules/testing.md"} {
		if !strings.Contains(inspectOut, expected) {
			t.Errorf("inspect should list %q in agent layer, got:\n%s", expected, inspectOut)
		}
	}

	// Verify round-trip: open into fresh dir and check files.
	dst := t.TempDir()
	run(t, dir, "open", "cp-1", dst)

	for _, rel := range []string{"CLAUDE.md", ".claude/settings.json", ".claude/rules/testing.md", ".claude/settings.local.json"} {
		srcData, err := os.ReadFile(filepath.Join(dir, rel))
		if err != nil {
			t.Fatalf("reading src %s: %v", rel, err)
		}
		dstData, err := os.ReadFile(filepath.Join(dst, rel))
		if err != nil {
			t.Fatalf("reading dst %s: %v", rel, err)
		}
		if string(srcData) != string(dstData) {
			t.Errorf("file %s mismatch after restore:\nsrc: %q\ndst: %q", rel, srcData, dstData)
		}
	}
}

// ---------------------------------------------------------------------------
// TestCodexExtension: .codex/ files land in the agent layer
// ---------------------------------------------------------------------------

func TestCodexExtension(t *testing.T) {
	dir := makeWorkspace(t)

	// Seed Codex marker files.
	writeFile(t, dir, ".codex/instructions.md", "# Codex Instructions\nUse TypeScript.\n")
	writeFile(t, dir, ".codex/setup.sh", "#!/bin/sh\necho setup\n")
	writeFile(t, dir, ".codex/config.yaml", "model: o3\n")

	out := run(t, dir, "save", "--skip-secret-scan", "-m", "codex test")

	if !strings.Contains(out, "codex") {
		t.Logf("save output: %s", out)
	}

	// Inspect with --files to verify agent layer contents.
	inspectOut := run(t, dir, "inspect", "--files")

	for _, expected := range []string{".codex/instructions.md", ".codex/setup.sh", ".codex/config.yaml"} {
		if !strings.Contains(inspectOut, expected) {
			t.Errorf("inspect should list %q in agent layer, got:\n%s", expected, inspectOut)
		}
	}

	// Verify round-trip.
	dst := t.TempDir()
	run(t, dir, "open", "cp-1", dst)

	for _, rel := range []string{".codex/instructions.md", ".codex/setup.sh", ".codex/config.yaml"} {
		srcData, err := os.ReadFile(filepath.Join(dir, rel))
		if err != nil {
			t.Fatalf("reading src %s: %v", rel, err)
		}
		dstData, err := os.ReadFile(filepath.Join(dst, rel))
		if err != nil {
			t.Fatalf("reading dst %s: %v", rel, err)
		}
		if string(srcData) != string(dstData) {
			t.Errorf("file %s mismatch after restore:\nsrc: %q\ndst: %q", rel, srcData, dstData)
		}
	}
}

// ---------------------------------------------------------------------------
// TestOpenCodeExtension: .opencode/ and opencode.json land in the agent layer
// ---------------------------------------------------------------------------

func TestOpenCodeExtension(t *testing.T) {
	dir := makeWorkspace(t)

	// Seed OpenCode marker files.
	writeFile(t, dir, ".opencode/commands/review.md", "Review the code for bugs.\n")
	writeFile(t, dir, "opencode.json", `{"data":{"directory":"~/.local/share/opencode"}}`)

	out := run(t, dir, "save", "--skip-secret-scan", "-m", "opencode test")

	if !strings.Contains(out, "opencode") {
		t.Logf("save output: %s", out)
	}

	// Inspect with --files to verify agent layer contents.
	inspectOut := run(t, dir, "inspect", "--files")

	for _, expected := range []string{".opencode/commands/review.md", "opencode.json"} {
		if !strings.Contains(inspectOut, expected) {
			t.Errorf("inspect should list %q in agent layer, got:\n%s", expected, inspectOut)
		}
	}

	// Verify round-trip.
	dst := t.TempDir()
	run(t, dir, "open", "cp-1", dst)

	for _, rel := range []string{".opencode/commands/review.md", "opencode.json"} {
		srcData, err := os.ReadFile(filepath.Join(dir, rel))
		if err != nil {
			t.Fatalf("reading src %s: %v", rel, err)
		}
		dstData, err := os.ReadFile(filepath.Join(dst, rel))
		if err != nil {
			t.Fatalf("reading dst %s: %v", rel, err)
		}
		if string(srcData) != string(dstData) {
			t.Errorf("file %s mismatch after restore:\nsrc: %q\ndst: %q", rel, srcData, dstData)
		}
	}
}

// ---------------------------------------------------------------------------
// TestOpenClawExtension: SOUL.md, MEMORY.md, memory/, skills/ land in agent layer
// ---------------------------------------------------------------------------

func TestOpenClawExtension(t *testing.T) {
	dir := makeWorkspace(t)

	// Seed OpenClaw marker files.
	writeFile(t, dir, "SOUL.md", "# Molty\nYou are a helpful lobster assistant.\n")
	writeFile(t, dir, "IDENTITY.md", "# Identity\nName: Molty\n")
	writeFile(t, dir, "MEMORY.md", "# Memory\n- User prefers Go.\n")
	writeFile(t, dir, "memory/debugging.md", "# Debugging Notes\nCheck logs first.\n")
	writeFile(t, dir, "skills/code-review/SKILL.md", "# Code Review Skill\nReview for bugs.\n")
	writeFile(t, dir, "canvas/diagram.md", "# Architecture\nGateway → Agent\n")

	out := run(t, dir, "save", "--skip-secret-scan", "-m", "openclaw test")

	if !strings.Contains(out, "openclaw") {
		t.Logf("save output: %s", out)
	}

	// Inspect with --files to verify agent layer contents.
	inspectOut := run(t, dir, "inspect", "--files")

	for _, expected := range []string{
		"SOUL.md", "IDENTITY.md", "MEMORY.md",
		"memory/debugging.md",
		"skills/code-review/SKILL.md",
		"canvas/diagram.md",
	} {
		if !strings.Contains(inspectOut, expected) {
			t.Errorf("inspect should list %q in agent layer, got:\n%s", expected, inspectOut)
		}
	}

	// Verify round-trip.
	dst := t.TempDir()
	run(t, dir, "open", "cp-1", dst)

	for _, rel := range []string{
		"SOUL.md", "IDENTITY.md", "MEMORY.md",
		"memory/debugging.md",
		"skills/code-review/SKILL.md",
		"canvas/diagram.md",
	} {
		srcData, err := os.ReadFile(filepath.Join(dir, rel))
		if err != nil {
			t.Fatalf("reading src %s: %v", rel, err)
		}
		dstData, err := os.ReadFile(filepath.Join(dst, rel))
		if err != nil {
			t.Fatalf("reading dst %s: %v", rel, err)
		}
		if string(srcData) != string(dstData) {
			t.Errorf("file %s mismatch after restore:\nsrc: %q\ndst: %q", rel, srcData, dstData)
		}
	}
}

// ---------------------------------------------------------------------------
// TestOpenClawCredentialsExcluded: ~/.openclaw/credentials/ must never be captured
// ---------------------------------------------------------------------------

func TestOpenClawCredentialsExcluded(t *testing.T) {
	dir := makeWorkspace(t)

	// Create a fake ~/.openclaw home with credentials.
	openclawHome := t.TempDir()
	t.Setenv("OPENCLAW_STATE_DIR", openclawHome)

	writeFile(t, openclawHome, "openclaw.json", `{"agent":{"model":"anthropic/claude-opus-4-6"}}`)
	writeFile(t, openclawHome, "credentials/whatsapp.json", `{"token":"secret-token-123"}`)
	writeFile(t, openclawHome, "credentials/telegram.json", `{"botToken":"bot-secret-456"}`)

	// Seed OpenClaw workspace marker.
	writeFile(t, dir, "SOUL.md", "# Molty\n")

	run(t, dir, "save", "--skip-secret-scan", "-m", "creds test")

	// Inspect with --files — credentials must NOT appear anywhere.
	inspectOut := run(t, dir, "inspect", "--files")

	for _, forbidden := range []string{"whatsapp.json", "telegram.json"} {
		if strings.Contains(inspectOut, forbidden) {
			t.Errorf("credentials file %q should be excluded from checkpoint, but found in:\n%s", forbidden, inspectOut)
		}
	}

	// The openclaw.json config SHOULD be captured (it's not a secret).
	if !strings.Contains(inspectOut, "openclaw.json") {
		t.Errorf("openclaw.json should be included in checkpoint, got:\n%s", inspectOut)
	}
}

// ---------------------------------------------------------------------------
// TestMultiAgentDetection: workspace with multiple agent markers detects all
// ---------------------------------------------------------------------------

func TestMultiAgentDetection(t *testing.T) {
	dir := makeWorkspace(t)

	// Seed markers for both Claude Code and OpenClaw.
	writeFile(t, dir, "CLAUDE.md", "# Claude\n")
	writeFile(t, dir, ".claude/settings.json", `{}`)
	writeFile(t, dir, "SOUL.md", "# Molty\n")
	writeFile(t, dir, "memory/notes.md", "# Notes\n")
	writeFile(t, dir, "AGENTS.md", "# Agents\n")

	run(t, dir, "save", "--skip-secret-scan", "-m", "multi-agent")

	inspectOut := run(t, dir, "inspect", "--files")

	// All agent files should be captured.
	for _, expected := range []string{
		"CLAUDE.md", ".claude/settings.json",
		"SOUL.md", "memory/notes.md",
		"AGENTS.md",
	} {
		if !strings.Contains(inspectOut, expected) {
			t.Errorf("inspect should list %q, got:\n%s", expected, inspectOut)
		}
	}

	// Verify round-trip.
	dst := t.TempDir()
	run(t, dir, "open", "cp-1", dst)

	for _, rel := range []string{"CLAUDE.md", ".claude/settings.json", "SOUL.md", "memory/notes.md", "AGENTS.md"} {
		if _, err := os.Stat(filepath.Join(dst, rel)); err != nil {
			t.Errorf("file %q should exist in restored workspace: %v", rel, err)
		}
	}
}

// ---------------------------------------------------------------------------
// TestAgentLayerReuse: unchanged agent files reuse layer digest across saves
// ---------------------------------------------------------------------------

func TestAgentLayerReuse(t *testing.T) {
	dir := makeWorkspace(t)

	// Seed Claude Code files.
	writeFile(t, dir, "CLAUDE.md", "# Claude\nStable instructions.\n")
	writeFile(t, dir, ".claude/settings.json", `{"model":"claude-sonnet-4-20250514"}`)

	run(t, dir, "save", "--skip-secret-scan", "-m", "save-1")

	// Modify only a project file — agent files unchanged.
	writeFile(t, dir, "main.go", "package main\n\nfunc main() { println(\"changed\") }\n")
	run(t, dir, "save", "--skip-secret-scan", "-m", "save-2")

	// Inspect both checkpoints and compare agent layer digests.
	inspect1 := run(t, dir, "inspect", "cp-1")
	inspect2 := run(t, dir, "inspect", "cp-2")

	digest1 := extractAgentLayerDigest(inspect1)
	digest2 := extractAgentLayerDigest(inspect2)

	if digest1 == "" {
		t.Fatal("could not extract agent layer digest from cp-1")
	}
	if digest1 != digest2 {
		t.Errorf("agent layer digest should be reused when agent files unchanged\ncp-1: %s\ncp-2: %s", digest1, digest2)
	}
}

// extractAgentLayerDigest finds the digest line following the agent layer header.
func extractAgentLayerDigest(inspectOutput string) string {
	lines := strings.Split(inspectOutput, "\n")
	for i, line := range lines {
		if strings.Contains(line, "] agent") {
			// The next line should contain the digest.
			if i+1 < len(lines) {
				digestLine := strings.TrimSpace(lines[i+1])
				// Extract the sha256:... portion.
				if idx := strings.Index(digestLine, "sha256:"); idx >= 0 {
					return digestLine[idx:]
				}
			}
		}
	}
	return ""
}
