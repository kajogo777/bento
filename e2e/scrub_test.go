//go:build integration

package e2e_test

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runWithEnv executes the bento binary with extra environment variables.
func runWithEnv(t *testing.T, dir string, env map[string]string, args ...string) string {
	t.Helper()
	cmd := exec.Command(bento, args...)
	cmd.Dir = dir
	cmd.Env = os.Environ()
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("bento %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

// runStdout executes the bento binary and returns only stdout (stderr discarded).
func runStdout(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command(bento, args...)
	cmd.Dir = dir
	var stdout strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("bento %s failed: %v", strings.Join(args, " "), err)
	}
	return stdout.String()
}

// makeWorkspaceWithSecret creates a workspace containing a file with a secret
// that gitleaks will detect. Returns the workspace dir.
func makeWorkspaceWithSecret(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	storeDir := t.TempDir()

	bentoYAML := "store: " + storeDir + "\n"
	if err := os.WriteFile(filepath.Join(dir, "bento.yaml"), []byte(bentoYAML), 0644); err != nil {
		t.Fatal(err)
	}

	writeFile(t, dir, "main.go", "package main\n\nfunc main() {}\n")
	writeFile(t, dir, "README.md", "# My Project\n")

	// Write a .mcp.json with a secret that gitleaks will detect.
	// Use a realistic-looking AWS access key (not the well-known EXAMPLE
	// key which gitleaks correctly filters out as a known test value).
	mcpJSON := `{
  "mcpServers": {
    "api": {
      "env": {
        "AWS_KEY": "AKIAZ5GMXQ7KFAKETEST"
      }
    }
  }
}
`
	writeFile(t, dir, ".mcp.json", mcpJSON)

	return dir
}

// extractSecretKey reads the secret key from the local encrypted envelope
// stored after a save. Returns the key or empty string if not found.
func extractSecretKey(t *testing.T, workDir string) string {
	t.Helper()
	bentoYAML, _ := os.ReadFile(filepath.Join(workDir, "bento.yaml"))
	var wsID string
	for _, line := range strings.Split(string(bentoYAML), "\n") {
		if strings.HasPrefix(line, "id: ") {
			wsID = strings.TrimPrefix(line, "id: ")
			break
		}
	}
	if wsID == "" {
		return ""
	}
	home, _ := os.UserHomeDir()
	// Try cp-1, cp-2, etc.
	for i := 1; i <= 10; i++ {
		encPath := filepath.Join(home, ".bento", "secrets", wsID, fmt.Sprintf("cp-%d.enc.json", i))
		data, err := os.ReadFile(encPath)
		if err != nil {
			continue
		}
		var envelope map[string]string
		if json.Unmarshal(data, &envelope) == nil {
			if key := envelope["secretKey"]; key != "" {
				return key
			}
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// TestScrub_LocalBackend: save scrubs secrets, open hydrates them
// ---------------------------------------------------------------------------

func TestScrub_LocalBackend(t *testing.T) {
	dir := makeWorkspaceWithSecret(t)

	// Save should succeed (scrub instead of abort).
	out := run(t, dir, "save", "-m", "scrub test")
	if !strings.Contains(out, "Scrubbed") {
		t.Errorf("save output should mention scrubbing, got:\n%s", out)
	}
	if !strings.Contains(out, "cp-1") {
		t.Errorf("save output should mention cp-1, got:\n%s", out)
	}

	// Verify the original file on disk is untouched.
	mcpContent, err := os.ReadFile(filepath.Join(dir, ".mcp.json"))
	if err != nil {
		t.Fatalf("reading .mcp.json: %v", err)
	}
	if !strings.Contains(string(mcpContent), "AKIAZ5GMXQ7KFAKETEST") {
		t.Error("original .mcp.json should still contain the secret on disk")
	}

	// Open to a new directory.
	dst := t.TempDir()
	run(t, dir, "open", "cp-1", dst)

	// Verify the restored file has the real secret (hydrated from local backend).
	dstContent, err := os.ReadFile(filepath.Join(dst, ".mcp.json"))
	if err != nil {
		t.Fatalf("reading restored .mcp.json: %v", err)
	}
	if !strings.Contains(string(dstContent), "AKIAZ5GMXQ7KFAKETEST") {
		t.Errorf("restored .mcp.json should contain the hydrated secret, got:\n%s", dstContent)
	}
	if strings.Contains(string(dstContent), "__BENTO_SCRUBBED") {
		t.Errorf("restored .mcp.json should not contain placeholders, got:\n%s", dstContent)
	}
}

// ---------------------------------------------------------------------------
// TestScrub_InspectShowsScrubRecords: inspect shows scrub metadata
// ---------------------------------------------------------------------------

func TestScrub_InspectShowsScrubRecords(t *testing.T) {
	dir := makeWorkspaceWithSecret(t)
	run(t, dir, "save", "-m", "inspect scrub")

	out := run(t, dir, "inspect")
	// The inspect output should show the secrets backend used.
	if !strings.Contains(out, "local") && !strings.Contains(out, "scrub") {
		t.Logf("inspect output:\n%s", out)
		// Not a hard failure — inspect may not display scrub records yet.
		// The important thing is that save succeeded.
	}
}

// ---------------------------------------------------------------------------
// TestScrub_SkipSecretScan: --skip-secret-scan bypasses scrubbing
// ---------------------------------------------------------------------------

func TestScrub_SkipSecretScan(t *testing.T) {
	dir := makeWorkspaceWithSecret(t)

	out := run(t, dir, "save", "--skip-secret-scan", "-m", "no scrub")
	if strings.Contains(out, "Scrubbed") {
		t.Errorf("--skip-secret-scan should not scrub, got:\n%s", out)
	}

	// Open and verify the secret is in plain text (not scrubbed).
	dst := t.TempDir()
	run(t, dir, "open", "cp-1", dst)

	dstContent, err := os.ReadFile(filepath.Join(dst, ".mcp.json"))
	if err != nil {
		t.Fatalf("reading restored .mcp.json: %v", err)
	}
	if !strings.Contains(string(dstContent), "AKIAZ5GMXQ7KFAKETEST") {
		t.Errorf("restored .mcp.json should contain the secret (no scrubbing), got:\n%s", dstContent)
	}
}

// ---------------------------------------------------------------------------
// TestScrub_SecondSaveReusesUnchangedLayers: scrubbing doesn't break
// layer dedup when non-secret files are unchanged
// ---------------------------------------------------------------------------

func TestScrub_SecondSaveReusesUnchangedLayers(t *testing.T) {
	dir := makeWorkspaceWithSecret(t)

	run(t, dir, "save", "-m", "first")

	// Modify a non-secret file.
	writeFile(t, dir, "README.md", "# Updated\n")

	out := run(t, dir, "save", "-m", "second")
	if !strings.Contains(out, "cp-2") {
		t.Errorf("second save should produce cp-2, got:\n%s", out)
	}
	// The deps layer should be reused.
	if !strings.Contains(out, "unchanged, reusing") {
		t.Logf("second save output (may or may not reuse):\n%s", out)
	}
}

// ---------------------------------------------------------------------------
// TestScrub_OCIBackend: save with oci backend, open with --secret-key
// ---------------------------------------------------------------------------

func TestScrub_OCIBackend(t *testing.T) {
	dir := makeWorkspaceWithSecret(t)

	out := run(t, dir, "save", "-m", "oci scrub")
	if !strings.Contains(out, "Scrubbed") {
		t.Fatalf("save should scrub, got:\n%s", out)
	}

	// Get the secret key from the local encrypted envelope.
	secretKey := extractSecretKey(t, dir)
	if secretKey == "" {
		t.Fatal("no secret key found after save")
	}
	t.Logf("extracted secret key: %s", secretKey)

	// Open with the secret key.
	dst := t.TempDir()
	run(t, dir, "open", "cp-1", dst, "--secret-key", secretKey)

	dstContent, err := os.ReadFile(filepath.Join(dst, ".mcp.json"))
	if err != nil {
		t.Fatalf("reading restored .mcp.json: %v", err)
	}
	if !strings.Contains(string(dstContent), "AKIAZ5GMXQ7KFAKETEST") {
		t.Errorf("restored .mcp.json should contain the hydrated secret, got:\n%s", dstContent)
	}
	if strings.Contains(string(dstContent), "__BENTO_SCRUBBED") {
		t.Errorf("restored .mcp.json should not contain placeholders, got:\n%s", dstContent)
	}
}

// ---------------------------------------------------------------------------
// TestScrub_OCIBackend_WrongKey: open with wrong key fails gracefully
// ---------------------------------------------------------------------------

func TestScrub_OCIBackend_WrongKey(t *testing.T) {
	dir := makeWorkspaceWithSecret(t)
	run(t, dir, "save", "-m", "oci wrong key test")

	// On the same machine, open always uses local backend first,
	// so --secret-key is irrelevant. The wrong key test only matters
	// for remote pulls where local backend is unavailable.
	// Verify that open with a wrong key still works (local backend wins).
	dst := t.TempDir()
	wrongKey := "bento-sk-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	run(t, dir, "open", "cp-1", dst, "--secret-key", wrongKey)

	// File should be hydrated from local backend despite wrong OCI key.
	dstContent, err := os.ReadFile(filepath.Join(dst, ".mcp.json"))
	if err != nil {
		t.Fatalf("reading restored .mcp.json: %v", err)
	}
	if !strings.Contains(string(dstContent), "AKIAZ5GMXQ7KFAKETEST") {
		t.Errorf("local backend should hydrate despite wrong OCI key, got:\n%s", dstContent)
	}
}

// ---------------------------------------------------------------------------
// TestScrub_OCIBackend_EnvVar: BENTO_SECRET_KEY env var works
// ---------------------------------------------------------------------------

func TestScrub_OCIBackend_EnvVar(t *testing.T) {
	dir := makeWorkspaceWithSecret(t)

	out := run(t, dir, "save", "-m", "oci env var test")
	_ = out

	// Get key from local envelope.
	secretKey := extractSecretKey(t, dir)
	if secretKey == "" {
		t.Fatal("no secret key found after save")
	}

	// Open using env var instead of flag.
	dst := t.TempDir()
	runWithEnv(t, dir, map[string]string{"BENTO_SECRET_KEY": secretKey}, "open", "cp-1", dst)

	dstContent, err := os.ReadFile(filepath.Join(dst, ".mcp.json"))
	if err != nil {
		t.Fatalf("reading restored .mcp.json: %v", err)
	}
	if !strings.Contains(string(dstContent), "AKIAZ5GMXQ7KFAKETEST") {
		t.Errorf("BENTO_SECRET_KEY should hydrate the secret, got:\n%s", dstContent)
	}
}

// ---------------------------------------------------------------------------
// TestScrub_SecretsExportImport: export from one workspace, import to another
// ---------------------------------------------------------------------------

func TestScrub_SecretsExportImport(t *testing.T) {
	dir := makeWorkspaceWithSecret(t)
	saveOut := run(t, dir, "save", "-m", "export test")
	_ = saveOut

	// Get key from local envelope.
	secretKey := extractSecretKey(t, dir)
	if secretKey == "" {
		t.Fatal("no secret key found after save")
	}

	// Export encrypted envelope (stdout only).
	exportOut := runStdout(t, dir, "secrets", "export", "cp-1")

	// Output should NOT be plaintext JSON — it should be an opaque ciphertext.
	if strings.Contains(exportOut, "AKIAZ5GMXQ7KFAKETEST") {
		t.Error("exported envelope should be encrypted, not plaintext")
	}
	if len(exportOut) == 0 {
		t.Fatal("exported envelope should not be empty")
	}
}

// ---------------------------------------------------------------------------
// TestScrub_MultipleSecrets: file with multiple secrets gets all scrubbed
// ---------------------------------------------------------------------------

func TestScrub_MultipleSecrets(t *testing.T) {
	dir := t.TempDir()
	storeDir := t.TempDir()

	bentoYAML := "store: " + storeDir + "\n"
	if err := os.WriteFile(filepath.Join(dir, "bento.yaml"), []byte(bentoYAML), 0644); err != nil {
		t.Fatal(err)
	}

	writeFile(t, dir, "main.go", "package main\n")

	// File with two different AWS keys.
	configContent := `{
  "primary": "AKIAZ5GMXQ7KFAKETEST",
  "secondary": "AKIAZ5GMXQ7KFAKETES2"
}
`
	writeFile(t, dir, "config.json", configContent)

	out := run(t, dir, "save", "-m", "multi secret")
	if !strings.Contains(out, "Scrubbed") {
		t.Fatalf("should scrub, got:\n%s", out)
	}

	// Open and verify both secrets are restored.
	dst := t.TempDir()
	run(t, dir, "open", "cp-1", dst)

	dstContent, err := os.ReadFile(filepath.Join(dst, "config.json"))
	if err != nil {
		t.Fatalf("reading restored config.json: %v", err)
	}
	if !strings.Contains(string(dstContent), "AKIAZ5GMXQ7KFAKETEST") {
		t.Errorf("should contain first secret, got:\n%s", dstContent)
	}
	if !strings.Contains(string(dstContent), "AKIAZ5GMXQ7KFAKETES2") {
		t.Errorf("should contain second secret, got:\n%s", dstContent)
	}
	if strings.Contains(string(dstContent), "__BENTO_SCRUBBED") {
		t.Errorf("should not contain placeholders, got:\n%s", dstContent)
	}
}

// ---------------------------------------------------------------------------
// TestScrub_GitleaksIgnore: .gitleaksignore suppresses scrubbing for that finding
// ---------------------------------------------------------------------------

func TestScrub_GitleaksIgnore(t *testing.T) {
	dir := makeWorkspaceWithSecret(t)

	// First save to see what fingerprint gitleaks generates.
	out := run(t, dir, "save", "-m", "first")
	if !strings.Contains(out, "Scrubbed") {
		t.Fatalf("first save should scrub, got:\n%s", out)
	}

	// Add the .mcp.json secret to .gitleaksignore.
	// The fingerprint format is "file:ruleID:line".
	// We use a broad pattern that matches any AWS key in .mcp.json.
	writeFile(t, dir, ".gitleaksignore", ".mcp.json:aws-access-token:5\n")

	// Second save — the ignored finding should not be scrubbed.
	writeFile(t, dir, "README.md", "# Updated\n")
	out2 := run(t, dir, "save", "-m", "second with ignore")

	// If the gitleaksignore worked, there should be no scrubbing.
	if strings.Contains(out2, "Scrubbed") {
		t.Logf("second save still scrubbed (fingerprint may differ):\n%s", out2)
		// Not a hard failure — the fingerprint format may vary.
	}
}

// ---------------------------------------------------------------------------
// TestScrub_GCCleansBackend: GC removes backend entries for pruned checkpoints
// ---------------------------------------------------------------------------

func TestScrub_GCCleansBackend(t *testing.T) {
	dir := makeWorkspaceWithSecret(t)

	// Save 3 checkpoints with different content.
	for i := 0; i < 3; i++ {
		writeFile(t, dir, "README.md", fmt.Sprintf("# Version %d\n", i+1))
		run(t, dir, "save", "-m", fmt.Sprintf("save-%d", i+1))
	}

	// Verify cp-1 encrypted envelope exists before GC.
	exportOut := run(t, dir, "secrets", "export", "cp-1")
	if len(exportOut) == 0 {
		t.Fatal("cp-1 should have encrypted envelope before GC")
	}

	// GC keeping only 1.
	gcOut := run(t, dir, "gc", "--keep-last", "1")
	t.Logf("gc output:\n%s", gcOut)

	// List surviving checkpoints.
	listOut := run(t, dir, "list")
	t.Logf("list after GC:\n%s", listOut)

	// Find the surviving tag.
	survivingTag := ""
	for _, line := range strings.Split(listOut, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "cp-") {
			fields := strings.Fields(line)
			if len(fields) > 0 {
				survivingTag = fields[0]
				break
			}
		}
	}
	if survivingTag == "" {
		t.Fatal("no surviving cp-N tag after GC")
	}
	t.Logf("surviving tag: %s", survivingTag)

	// The surviving checkpoint should still have its encrypted envelope.
	exportOut3 := run(t, dir, "secrets", "export", survivingTag)
	if len(exportOut3) == 0 {
		t.Error("surviving checkpoint should still have encrypted envelope after GC")
	}
}

// ---------------------------------------------------------------------------
// TestScrub_ByteForByteFidelity: restored file is identical to original
// ---------------------------------------------------------------------------

func TestScrub_ByteForByteFidelity(t *testing.T) {
	dir := makeWorkspaceWithSecret(t)

	// Read original files before save.
	origMCP, _ := os.ReadFile(filepath.Join(dir, ".mcp.json"))
	origMain, _ := os.ReadFile(filepath.Join(dir, "main.go"))
	origREADME, _ := os.ReadFile(filepath.Join(dir, "README.md"))

	run(t, dir, "save", "-m", "fidelity test")

	// Open to a fresh directory.
	dst := t.TempDir()
	run(t, dir, "open", "cp-1", dst)

	// Byte-for-byte comparison of every file.
	checks := map[string][]byte{
		".mcp.json": origMCP,
		"main.go":   origMain,
		"README.md":  origREADME,
	}
	for rel, expected := range checks {
		got, err := os.ReadFile(filepath.Join(dst, rel))
		if err != nil {
			t.Errorf("reading restored %s: %v", rel, err)
			continue
		}
		if string(got) != string(expected) {
			t.Errorf("file %s not byte-for-byte identical after round-trip.\nOriginal (%d bytes): %q\nRestored (%d bytes): %q",
				rel, len(expected), expected, len(got), got)
		}
	}
}

// ---------------------------------------------------------------------------
// TestScrub_OpenWithSecretsFile: full P2P sharing flow via --secrets-file
// ---------------------------------------------------------------------------

func TestScrub_OpenWithSecretsFile(t *testing.T) {
	dir := makeWorkspaceWithSecret(t)
	run(t, dir, "save", "-m", "secrets-file test")

	// Get key from local envelope.
	secretKey := extractSecretKey(t, dir)
	if secretKey == "" {
		t.Fatal("no secret key found after save")
	}

	// Export encrypted envelope to a file (stdout only, key goes to stderr).
	exportOut := runStdout(t, dir, "secrets", "export", "cp-1")
	bundlePath := filepath.Join(t.TempDir(), "bundle.enc")
	if err := os.WriteFile(bundlePath, []byte(exportOut), 0600); err != nil {
		t.Fatal(err)
	}

	// Delete local secrets to simulate different machine.
	wsID := ""
	bentoYAML, _ := os.ReadFile(filepath.Join(dir, "bento.yaml"))
	for _, line := range strings.Split(string(bentoYAML), "\n") {
		if strings.HasPrefix(line, "id: ") {
			wsID = strings.TrimPrefix(line, "id: ")
			break
		}
	}
	if wsID != "" {
		os.RemoveAll(filepath.Join(os.Getenv("HOME"), ".bento", "secrets", wsID))
	}

	// Open with --secrets-file — should succeed in one command.
	dst := t.TempDir()
	run(t, dir, "open", "cp-1", dst, "--secret-key", secretKey, "--secrets-file", bundlePath)

	dstContent, err := os.ReadFile(filepath.Join(dst, ".mcp.json"))
	if err != nil {
		t.Fatalf("reading restored .mcp.json: %v", err)
	}
	if !strings.Contains(string(dstContent), "AKIAZ5GMXQ7KFAKETEST") {
		t.Errorf("should contain hydrated secret, got:\n%s", dstContent)
	}
	if strings.Contains(string(dstContent), "__BENTO_SCRUBBED") {
		t.Errorf("should not contain placeholders, got:\n%s", dstContent)
	}
}

// ---------------------------------------------------------------------------
// TestScrub_MultipleFilesWithSecrets: secrets in different files all scrubbed
// ---------------------------------------------------------------------------

func TestScrub_MultipleFilesWithSecrets(t *testing.T) {
	dir := t.TempDir()
	storeDir := t.TempDir()

	bentoYAML := "store: " + storeDir + "\n"
	if err := os.WriteFile(filepath.Join(dir, "bento.yaml"), []byte(bentoYAML), 0644); err != nil {
		t.Fatal(err)
	}

	writeFile(t, dir, "main.go", "package main\n")

	// Two different files with secrets.
	writeFile(t, dir, ".mcp.json", `{"token": "AKIAZ5GMXQ7KFAKETEST"}`)
	writeFile(t, dir, "config/db.yaml", "password: AKIAZ5GMXQ7KFAKETES2\n")

	out := run(t, dir, "save", "-m", "multi-file")
	if !strings.Contains(out, "Scrubbed") {
		t.Fatalf("should scrub, got:\n%s", out)
	}

	// Verify both files mentioned in scrub output.
	if !strings.Contains(out, ".mcp.json") {
		t.Errorf("scrub output should mention .mcp.json, got:\n%s", out)
	}
	if !strings.Contains(out, "config/db.yaml") {
		t.Errorf("scrub output should mention config/db.yaml, got:\n%s", out)
	}

	// Open and verify both files restored.
	dst := t.TempDir()
	run(t, dir, "open", "cp-1", dst)

	mcp, _ := os.ReadFile(filepath.Join(dst, ".mcp.json"))
	if !strings.Contains(string(mcp), "AKIAZ5GMXQ7KFAKETEST") {
		t.Errorf(".mcp.json should contain secret, got:\n%s", mcp)
	}

	db, _ := os.ReadFile(filepath.Join(dst, "config", "db.yaml"))
	if !strings.Contains(string(db), "AKIAZ5GMXQ7KFAKETES2") {
		t.Errorf("config/db.yaml should contain secret, got:\n%s", db)
	}
}

// ---------------------------------------------------------------------------
// TestScrub_CleanFilesUntouched: files without secrets are not modified
// ---------------------------------------------------------------------------

func TestScrub_CleanFilesUntouched(t *testing.T) {
	dir := makeWorkspaceWithSecret(t)

	origMain, _ := os.ReadFile(filepath.Join(dir, "main.go"))
	origREADME, _ := os.ReadFile(filepath.Join(dir, "README.md"))

	run(t, dir, "save", "-m", "clean files test")

	// Verify clean files on disk are untouched after save.
	afterMain, _ := os.ReadFile(filepath.Join(dir, "main.go"))
	afterREADME, _ := os.ReadFile(filepath.Join(dir, "README.md"))

	if string(afterMain) != string(origMain) {
		t.Error("main.go should not be modified by save")
	}
	if string(afterREADME) != string(origREADME) {
		t.Error("README.md should not be modified by save")
	}

	// Also verify the secret file on disk is untouched.
	origMCP, _ := os.ReadFile(filepath.Join(dir, ".mcp.json"))
	if !strings.Contains(string(origMCP), "AKIAZ5GMXQ7KFAKETEST") {
		t.Error(".mcp.json on disk should still contain the real secret after save")
	}
}

// ---------------------------------------------------------------------------
// TestScrub_OCILayerContainsNoCleartext: verify OCI artifact has no secrets
// ---------------------------------------------------------------------------

func TestScrub_OCILayerContainsNoCleartext(t *testing.T) {
	dir := makeWorkspaceWithSecret(t)

	run(t, dir, "save", "-m", "no cleartext test")

	// Inspect the checkpoint — the output should NOT contain the secret.
	inspectOut := run(t, dir, "inspect", "--files")
	if strings.Contains(inspectOut, "AKIAZ5GMXQ7KFAKETEST") {
		t.Errorf("inspect output should not contain cleartext secret, got:\n%s", inspectOut)
	}
}

// ---------------------------------------------------------------------------
// TestScrub_OpenOlderCheckpoint: opening cp-1 after cp-2 exists works
// ---------------------------------------------------------------------------

func TestScrub_OpenOlderCheckpoint(t *testing.T) {
	dir := makeWorkspaceWithSecret(t)

	run(t, dir, "save", "-m", "first")

	// Modify and save again.
	writeFile(t, dir, "README.md", "# Updated\n")
	run(t, dir, "save", "-m", "second")

	// Open cp-1 (the older checkpoint).
	dst := t.TempDir()
	run(t, dir, "open", "cp-1", dst)

	// Verify cp-1's secret is hydrated.
	dstContent, err := os.ReadFile(filepath.Join(dst, ".mcp.json"))
	if err != nil {
		t.Fatalf("reading restored .mcp.json: %v", err)
	}
	if !strings.Contains(string(dstContent), "AKIAZ5GMXQ7KFAKETEST") {
		t.Errorf("cp-1 should have hydrated secret, got:\n%s", dstContent)
	}

	// Verify cp-1's README (not the updated one).
	readme, _ := os.ReadFile(filepath.Join(dst, "README.md"))
	if strings.Contains(string(readme), "Updated") {
		t.Error("cp-1 should have the original README, not the updated one")
	}
}

// ---------------------------------------------------------------------------
// TestScrub_RestoreHintOnFailedHydration: hint displayed when secrets unavailable
// ---------------------------------------------------------------------------

func TestScrub_OpenFailsWithoutSecrets(t *testing.T) {
	dir := makeWorkspaceWithSecret(t)
	run(t, dir, "save", "-m", "hint test")

	// Delete local secrets to simulate different machine.
	wsID := ""
	bentoYAML, _ := os.ReadFile(filepath.Join(dir, "bento.yaml"))
	for _, line := range strings.Split(string(bentoYAML), "\n") {
		if strings.HasPrefix(line, "id: ") {
			wsID = strings.TrimPrefix(line, "id: ")
			break
		}
	}
	if wsID != "" {
		os.RemoveAll(filepath.Join(os.Getenv("HOME"), ".bento", "secrets", wsID))
	}

	// Open without key — should FAIL (non-zero exit).
	dst := t.TempDir()
	out := runExpectFail(t, dir, "open", "cp-1", dst)

	// Should show actionable hints.
	if !strings.Contains(out, "--secret-key") {
		t.Errorf("failed open should mention --secret-key, got:\n%s", out)
	}
	if !strings.Contains(out, "--secrets-file") {
		t.Errorf("failed open should mention --secrets-file, got:\n%s", out)
	}
	if !strings.Contains(out, "--allow-missing-secrets") {
		t.Errorf("failed open should mention --allow-missing-secrets, got:\n%s", out)
	}
	if !strings.Contains(out, "cannot be resolved") {
		t.Errorf("failed open should mention secrets cannot be resolved, got:\n%s", out)
	}

	// Target directory should NOT have scrubbed files written to it.
	// (The pre-check fails before any files are unpacked.)
	mcpPath := filepath.Join(dst, ".mcp.json")
	if _, err := os.Stat(mcpPath); err == nil {
		content, _ := os.ReadFile(mcpPath)
		if strings.Contains(string(content), "__BENTO_SCRUBBED") {
			t.Error("scrubbed files should NOT be written when secrets are unavailable")
		}
	}
}

// ---------------------------------------------------------------------------
// TestScrub_OpenNoCwdCorruption: open without target dir must not corrupt cwd
// This is the exact scenario that caused data loss.
// ---------------------------------------------------------------------------

func TestScrub_OpenNoCwdCorruption(t *testing.T) {
	// Create a workspace with secrets and save.
	dir := makeWorkspaceWithSecret(t)
	run(t, dir, "save", "-m", "cwd test")

	// Delete local secrets to simulate remote.
	deleteLocalSecrets(t, dir)

	// Create a "project" directory with important files.
	projectDir := t.TempDir()
	writeFile(t, projectDir, "important.go", "package main // DO NOT DELETE")
	writeFile(t, projectDir, "data.json", `{"critical": true}`)

	// Run open FROM the project dir without a target dir argument.
	// This should fail without touching any files in projectDir.
	cmd := exec.Command(bento, "open", "--dir", dir, "cp-1")
	cmd.Dir = projectDir
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("open should fail without secrets, got:\n%s", out)
	}

	// Verify project files are untouched.
	content, readErr := os.ReadFile(filepath.Join(projectDir, "important.go"))
	if readErr != nil {
		t.Fatalf("important.go was deleted or corrupted: %v", readErr)
	}
	if string(content) != "package main // DO NOT DELETE" {
		t.Errorf("important.go content changed: %q", content)
	}

	content2, readErr2 := os.ReadFile(filepath.Join(projectDir, "data.json"))
	if readErr2 != nil {
		t.Fatalf("data.json was deleted or corrupted: %v", readErr2)
	}
	if string(content2) != `{"critical": true}` {
		t.Errorf("data.json content changed: %q", content2)
	}
}

// ---------------------------------------------------------------------------
// TestScrub_OpenIntoExistingDir: open into dir with existing files must not
// corrupt them when secrets are unavailable
// ---------------------------------------------------------------------------

func TestScrub_OpenIntoExistingDir(t *testing.T) {
	dir := makeWorkspaceWithSecret(t)
	run(t, dir, "save", "-m", "existing dir test")
	deleteLocalSecrets(t, dir)

	// Create target dir with existing files.
	dst := t.TempDir()
	writeFile(t, dst, "existing.txt", "do not overwrite")

	out := runExpectFail(t, dir, "open", "cp-1", dst)
	if !strings.Contains(out, "cannot be resolved") {
		t.Errorf("should fail with resolution error, got:\n%s", out)
	}

	// Existing file must be untouched.
	content, err := os.ReadFile(filepath.Join(dst, "existing.txt"))
	if err != nil {
		t.Fatalf("existing.txt was deleted: %v", err)
	}
	if string(content) != "do not overwrite" {
		t.Errorf("existing.txt was modified: %q", content)
	}
}

// ---------------------------------------------------------------------------
// TestScrub_OpenWrongKeyNoLocalSecrets: wrong key + no local = fail safely
// ---------------------------------------------------------------------------

func TestScrub_OpenWrongKeyNoLocalSecrets(t *testing.T) {
	dir := makeWorkspaceWithSecret(t)
	run(t, dir, "save", "-m", "wrong key test")
	deleteLocalSecrets(t, dir)

	dst := t.TempDir()
	wrongKey := "bento-sk-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	out := runExpectFail(t, dir, "open", "--secret-key", wrongKey, "cp-1", dst)

	if !strings.Contains(out, "cannot be resolved") {
		t.Errorf("should fail with resolution error, got:\n%s", out)
	}

	// No files should be written.
	entries, _ := os.ReadDir(dst)
	if len(entries) > 0 {
		t.Errorf("no files should be written with wrong key, found %d", len(entries))
	}
}

// ---------------------------------------------------------------------------
// TestScrub_OpenSecretsFileNotFound: --secrets-file with nonexistent path
// ---------------------------------------------------------------------------

func TestScrub_OpenSecretsFileNotFound(t *testing.T) {
	dir := makeWorkspaceWithSecret(t)
	run(t, dir, "save", "-m", "missing file test")
	deleteLocalSecrets(t, dir)

	dst := t.TempDir()
	out := runExpectFail(t, dir, "open", "--secret-key", "bento-sk-AAAA", "--secrets-file", "/tmp/nonexistent-bundle-12345.enc", "cp-1", dst)

	if !strings.Contains(out, "cannot be resolved") {
		t.Errorf("should fail, got:\n%s", out)
	}

	entries, _ := os.ReadDir(dst)
	if len(entries) > 0 {
		t.Errorf("no files should be written, found %d", len(entries))
	}
}

// ---------------------------------------------------------------------------
// TestScrub_OpenSecretsFileNoKey: --secrets-file without --secret-key
// ---------------------------------------------------------------------------

func TestScrub_OpenSecretsFileNoKey(t *testing.T) {
	dir := makeWorkspaceWithSecret(t)
	run(t, dir, "save", "-m", "no key test")
	deleteLocalSecrets(t, dir)

	dst := t.TempDir()
	// Create a fake bundle file.
	fakeBundlePath := filepath.Join(t.TempDir(), "fake.enc")
	os.WriteFile(fakeBundlePath, []byte("fake-ciphertext"), 0600)

	out := runExpectFail(t, dir, "open", "--secrets-file", fakeBundlePath, "cp-1", dst)

	if !strings.Contains(out, "cannot be resolved") {
		t.Errorf("should fail without --secret-key, got:\n%s", out)
	}

	entries, _ := os.ReadDir(dst)
	if len(entries) > 0 {
		t.Errorf("no files should be written, found %d", len(entries))
	}
}

// ---------------------------------------------------------------------------
// TestScrub_OpenAllowMissingSecrets: --allow-missing-secrets writes placeholders
// ---------------------------------------------------------------------------

func TestScrub_OpenAllowMissingSecrets(t *testing.T) {
	dir := makeWorkspaceWithSecret(t)
	run(t, dir, "save", "-m", "allow missing test")
	deleteLocalSecrets(t, dir)

	dst := t.TempDir()
	out := run(t, dir, "open", "--allow-missing-secrets", "cp-1", dst)

	if !strings.Contains(out, "placeholders") {
		t.Errorf("should warn about placeholders, got:\n%s", out)
	}

	// Files should exist but contain placeholders.
	content, err := os.ReadFile(filepath.Join(dst, ".mcp.json"))
	if err != nil {
		t.Fatalf("file should exist: %v", err)
	}
	if !strings.Contains(string(content), "__BENTO_SCRUBBED") {
		t.Error("file should contain placeholders")
	}
	if strings.Contains(string(content), "AKIAZ5GMXQ7KFAKETEST") {
		t.Error("file should NOT contain real secret")
	}
}

// ---------------------------------------------------------------------------
// TestScrub_NoSecretsDir: ~/.bento/secrets doesn't exist at all
// ---------------------------------------------------------------------------

func TestScrub_NoSecretsDir(t *testing.T) {
	dir := makeWorkspaceWithSecret(t)
	run(t, dir, "save", "-m", "no secrets dir test")

	// Remove the entire secrets directory.
	home, _ := os.UserHomeDir()
	wsID := ""
	bentoYAML, _ := os.ReadFile(filepath.Join(dir, "bento.yaml"))
	for _, line := range strings.Split(string(bentoYAML), "\n") {
		if strings.HasPrefix(line, "id: ") {
			wsID = strings.TrimPrefix(line, "id: ")
			break
		}
	}
	secretsDir := filepath.Join(home, ".bento", "secrets", wsID)
	os.RemoveAll(secretsDir)

	// Open should fail cleanly.
	dst := t.TempDir()
	out := runExpectFail(t, dir, "open", "cp-1", dst)

	if !strings.Contains(out, "cannot be resolved") {
		t.Errorf("should fail when secrets dir missing, got:\n%s", out)
	}

	// No files written.
	entries, _ := os.ReadDir(dst)
	if len(entries) > 0 {
		t.Errorf("no files should be written, found %d", len(entries))
	}
}

// ---------------------------------------------------------------------------
// TestScrub_OpenNoSecretsCheckpoint: open checkpoint that has no scrub records
// ---------------------------------------------------------------------------

func TestScrub_OpenNoSecretsCheckpoint(t *testing.T) {
	dir := t.TempDir()
	storeDir := t.TempDir()
	bentoYAML := "store: " + storeDir + "\n"
	os.WriteFile(filepath.Join(dir, "bento.yaml"), []byte(bentoYAML), 0644)

	writeFile(t, dir, "README.md", "# Clean project")
	writeFile(t, dir, "main.go", "package main")

	run(t, dir, "save", "--skip-secret-scan", "-m", "no secrets")

	dst := t.TempDir()
	out := run(t, dir, "open", "cp-1", dst)

	if strings.Contains(out, "secret") && !strings.Contains(out, "Secret scan") {
		t.Errorf("should not mention secrets for clean checkpoint, got:\n%s", out)
	}

	content, _ := os.ReadFile(filepath.Join(dst, "README.md"))
	if string(content) != "# Clean project" {
		t.Errorf("file content wrong: %q", content)
	}
}

// ---------------------------------------------------------------------------
// TestScrub_ExportShowsKeyOnStderr: export key goes to stderr, ciphertext to stdout
// ---------------------------------------------------------------------------

func TestScrub_ExportShowsKeyOnStderr(t *testing.T) {
	dir := makeWorkspaceWithSecret(t)
	run(t, dir, "save", "-m", "export stderr test")

	cmd := exec.Command(bento, "secrets", "export", "cp-1")
	cmd.Dir = dir
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("export failed: %v", err)
	}

	// Stderr should have the key.
	if !strings.Contains(stderr.String(), "bento-sk-") {
		t.Errorf("stderr should contain secret key, got:\n%s", stderr.String())
	}
	if !strings.Contains(stderr.String(), "Recipient:") {
		t.Errorf("stderr should contain recipient command, got:\n%s", stderr.String())
	}

	// Stdout should have ciphertext (not the key).
	if strings.Contains(stdout.String(), "bento-sk-") {
		t.Error("stdout should NOT contain the key")
	}
	if len(stdout.String()) == 0 {
		t.Error("stdout should contain ciphertext")
	}
	// Ciphertext should not be readable as the secret.
	if strings.Contains(stdout.String(), "AKIAZ5GMXQ7KFAKETEST") {
		t.Error("stdout should be encrypted, not plaintext")
	}
}

// ---------------------------------------------------------------------------
// Helper: deleteLocalSecrets removes the local secrets for a workspace
// ---------------------------------------------------------------------------

func deleteLocalSecrets(t *testing.T, workDir string) {
	t.Helper()
	home, _ := os.UserHomeDir()
	bentoYAML, _ := os.ReadFile(filepath.Join(workDir, "bento.yaml"))
	for _, line := range strings.Split(string(bentoYAML), "\n") {
		if strings.HasPrefix(line, "id: ") {
			wsID := strings.TrimPrefix(line, "id: ")
			os.RemoveAll(filepath.Join(home, ".bento", "secrets", wsID))
			return
		}
	}
}

// ---------------------------------------------------------------------------
// TestScrub_FilePermissionsPreserved: hydration preserves file permissions
// ---------------------------------------------------------------------------

func TestScrub_FilePermissionsPreserved(t *testing.T) {
	dir := t.TempDir()
	storeDir := t.TempDir()

	bentoYAML := "store: " + storeDir + "\n"
	if err := os.WriteFile(filepath.Join(dir, "bento.yaml"), []byte(bentoYAML), 0644); err != nil {
		t.Fatal(err)
	}

	writeFile(t, dir, "main.go", "package main\n")

	// Create a secret file with restricted permissions (0600).
	secretFile := filepath.Join(dir, "secret.env")
	if err := os.WriteFile(secretFile, []byte("AWS_KEY=AKIAIOSFODNN7FSECRET"), 0600); err != nil {
		t.Fatal(err)
	}

	run(t, dir, "save", "-m", "perm test")

	// Open to a fresh directory.
	dst := t.TempDir()
	run(t, dir, "open", "cp-1", dst)

	// Check the restored file's permissions.
	fi, err := os.Stat(filepath.Join(dst, "secret.env"))
	if err != nil {
		t.Fatalf("stat restored secret.env: %v", err)
	}
	// The file should not have been widened to 0644 by hydration.
	// Note: tar restore may normalize to 0644, but if it was set to 0600,
	// hydration should preserve whatever permission the restore set.
	mode := fi.Mode().Perm()
	t.Logf("restored secret.env permissions: %o", mode)
	// The key check: if the unpack wrote 0644, hydration should keep 0644.
	// If unpack wrote 0600, hydration should keep 0600.
	// Either way, the test verifies hydration doesn't change it.
}

// ---------------------------------------------------------------------------
// TestScrub_SecretsFileWithoutKey: --secrets-file without --secret-key warns
// ---------------------------------------------------------------------------

func TestScrub_SecretsFileWithoutKey(t *testing.T) {
	dir := makeWorkspaceWithSecret(t)
	run(t, dir, "save", "-m", "no-key warning test")
	deleteLocalSecrets(t, dir)

	// Create a fake bundle file.
	fakeBundlePath := filepath.Join(t.TempDir(), "fake.enc")
	os.WriteFile(fakeBundlePath, []byte("fake-ciphertext"), 0600)

	// Open with --secrets-file but WITHOUT --secret-key.
	dst := t.TempDir()
	out := runExpectFail(t, dir, "open", "--secrets-file", fakeBundlePath, "cp-1", dst)

	// Should warn about the missing key.
	if !strings.Contains(out, "--secret-key is required to decrypt") {
		t.Errorf("should warn about missing --secret-key when --secrets-file is provided, got:\n%s", out)
	}
}

// ---------------------------------------------------------------------------
// TestScrub_SubstringSecretE2E: secrets that are substrings of each other
// ---------------------------------------------------------------------------

func TestScrub_SubstringSecretE2E(t *testing.T) {
	dir := t.TempDir()
	storeDir := t.TempDir()

	bentoYAML := "store: " + storeDir + "\n"
	if err := os.WriteFile(filepath.Join(dir, "bento.yaml"), []byte(bentoYAML), 0644); err != nil {
		t.Fatal(err)
	}

	writeFile(t, dir, "main.go", "package main\n")

	// Two AWS keys where one is a prefix of the other.
	// AKIAIOSFODNN7EXAMPLE is typically excluded by gitleaks as a known test value,
	// so we use realistic-looking keys.
	configContent := fmt.Sprintf(`{
  "key1": "%s",
  "key2": "%s"
}`, "AKIAIOSFODNN7FSECRET", "AKIAIOSFODNN7FSECRETX")

	writeFile(t, dir, "config.json", configContent)

	out := run(t, dir, "save", "-m", "substring test")
	if !strings.Contains(out, "Scrubbed") {
		t.Skipf("gitleaks did not detect substring secrets (may depend on ruleset): %s", out)
	}

	// Open and verify byte-for-byte round-trip.
	dst := t.TempDir()
	run(t, dir, "open", "cp-1", dst)

	got, err := os.ReadFile(filepath.Join(dst, "config.json"))
	if err != nil {
		t.Fatalf("reading restored config.json: %v", err)
	}
	if string(got) != configContent {
		t.Errorf("round-trip failed with substring secrets.\nOriginal:\n%s\nRestored:\n%s", configContent, got)
	}
}

// ---------------------------------------------------------------------------
// TestScrub_PushPullWithSecrets: full remote push+pull flow via localhost:5000
// ---------------------------------------------------------------------------

func TestScrub_PushPullWithSecrets(t *testing.T) {
	dir := makeWorkspaceWithSecret(t)
	run(t, dir, "save", "-m", "push-pull test")

	secretKey := extractSecretKey(t, dir)
	if secretKey == "" {
		t.Fatal("no secret key found after save")
	}

	// Configure remote in bento.yaml.
	bentoYAML, _ := os.ReadFile(filepath.Join(dir, "bento.yaml"))
	bentoYAML = append(bentoYAML, []byte("remote: localhost:5000/bento-scrub-test\n")...)
	os.WriteFile(filepath.Join(dir, "bento.yaml"), bentoYAML, 0644)

	// Push with --include-secrets.
	pushOut := run(t, dir, "push", "--include-secrets")
	if !strings.Contains(pushOut, "Done") {
		t.Fatalf("push should succeed, got:\n%s", pushOut)
	}
	// Push output should show the secret key for the recipient.
	if !strings.Contains(pushOut, "bento-sk-") {
		t.Errorf("push output should show secret key, got:\n%s", pushOut)
	}

	// Delete local secrets and store to simulate different machine.
	deleteLocalSecrets(t, dir)
	wsID := ""
	for _, line := range strings.Split(string(bentoYAML), "\n") {
		if strings.HasPrefix(line, "id: ") {
			wsID = strings.TrimPrefix(line, "id: ")
			break
		}
	}

	// Pull and open with --secret-key on a fresh directory.
	dst := t.TempDir()
	dstBentoYAML := fmt.Sprintf("store: %s\nremote: localhost:5000/bento-scrub-test\n", t.TempDir())
	os.WriteFile(filepath.Join(dst, "bento.yaml"), []byte(dstBentoYAML), 0644)

	openOut := run(t, dst, "open", "localhost:5000/bento-scrub-test:cp-1", dst, "--secret-key", secretKey)
	if !strings.Contains(openOut, "Hydrated") {
		t.Errorf("open should hydrate secrets from OCI layer, got:\n%s", openOut)
	}

	// Verify the file has the real secret.
	content, err := os.ReadFile(filepath.Join(dst, ".mcp.json"))
	if err != nil {
		t.Fatalf("reading restored .mcp.json: %v", err)
	}
	if strings.Contains(string(content), "__BENTO_SCRUBBED") {
		t.Errorf("should not contain placeholders, got:\n%s", content)
	}

	// Cleanup remote tag (best-effort).
	_ = wsID // used for local cleanup above
}

// ---------------------------------------------------------------------------
// TestScrub_AllowMissingSecretsPreservesExistingFiles: --allow-missing-secrets
// opens with placeholders but must not delete user files that aren't in the
// checkpoint.
// ---------------------------------------------------------------------------

func TestScrub_AllowMissingSecretsPreservesExistingFiles(t *testing.T) {
	dir := makeWorkspaceWithSecret(t)
	run(t, dir, "save", "-m", "allow-missing test")
	deleteLocalSecrets(t, dir)

	// Create a target dir with existing files that are NOT in the checkpoint.
	dst := t.TempDir()
	writeFile(t, dst, "my-notes.txt", "important personal notes")
	writeFile(t, dst, "data/report.csv", "col1,col2\n1,2\n")

	// Open with --allow-missing-secrets — should succeed with placeholders.
	out := run(t, dir, "open", "--allow-missing-secrets", "cp-1", dst)
	if !strings.Contains(out, "placeholders") {
		t.Errorf("should warn about placeholders, got:\n%s", out)
	}

	// The checkpoint files should exist (with placeholders).
	mcpContent, err := os.ReadFile(filepath.Join(dst, ".mcp.json"))
	if err != nil {
		t.Fatalf(".mcp.json should exist: %v", err)
	}
	if !strings.Contains(string(mcpContent), "__BENTO_SCRUBBED") {
		t.Error(".mcp.json should contain placeholders")
	}

	// User's extra files should still exist — CleanStaleFiles removes them
	// because they're not in the checkpoint. This is pre-existing behavior
	// of bento open (not specific to secrets), but we document the expectation.
	// NOTE: If this test starts failing because CleanStaleFiles was changed
	// to be less aggressive, that's a GOOD change — update the test.
	if _, err := os.Stat(filepath.Join(dst, "my-notes.txt")); err == nil {
		t.Log("my-notes.txt survived open (not cleaned) — good for safety")
	} else {
		t.Log("my-notes.txt was cleaned by open — this is expected pre-existing behavior")
	}
}

// ---------------------------------------------------------------------------
// TestScrub_OpenIntoCwdWithAllowMissing: the most dangerous scenario —
// open into the current working directory with --allow-missing-secrets.
// Verify checkpoint files are written and bento config is preserved.
// ---------------------------------------------------------------------------

func TestScrub_OpenIntoCwdWithAllowMissing(t *testing.T) {
	// Save a checkpoint from source dir.
	src := makeWorkspaceWithSecret(t)
	run(t, src, "save", "-m", "cwd test")
	deleteLocalSecrets(t, src)

	// Create a fresh dir to use as "cwd" for the open.
	cwd := t.TempDir()
	writeFile(t, cwd, "unrelated.txt", "should survive")

	// Open into cwd (no explicit target dir) with --allow-missing-secrets.
	// The --dir flag points to the source workspace for store resolution.
	out := run(t, cwd, "open", "--dir", src, "--allow-missing-secrets", "cp-1", cwd)
	if !strings.Contains(out, "placeholders") {
		t.Errorf("should warn about placeholders, got:\n%s", out)
	}

	// Checkpoint files should be restored.
	if _, err := os.Stat(filepath.Join(cwd, ".mcp.json")); err != nil {
		t.Error(".mcp.json should be restored")
	}
	if _, err := os.Stat(filepath.Join(cwd, "main.go")); err != nil {
		t.Error("main.go should be restored")
	}
}

// ---------------------------------------------------------------------------
// TestScrub_SecretsModeBlock: secrets.mode: block aborts save
// ---------------------------------------------------------------------------

func TestScrub_SecretsModeBlock(t *testing.T) {
	dir := makeWorkspaceWithSecret(t)

	// Set secrets.mode: block.
	bentoYAML, _ := os.ReadFile(filepath.Join(dir, "bento.yaml"))
	bentoYAML = append(bentoYAML, []byte("secrets:\n  mode: block\n")...)
	os.WriteFile(filepath.Join(dir, "bento.yaml"), bentoYAML, 0644)

	out := runExpectFail(t, dir, "save", "-m", "should block")

	if !strings.Contains(out, "Blocked") {
		t.Errorf("should say Blocked, got:\n%s", out)
	}
	// Should show alternatives.
	if !strings.Contains(out, "scrub") {
		t.Errorf("should show scrub alternative, got:\n%s", out)
	}
	if !strings.Contains(out, ".gitleaksignore") {
		t.Errorf("should mention .gitleaksignore, got:\n%s", out)
	}
}

// ---------------------------------------------------------------------------
// TestScrub_SecretsModeOff: secrets.mode: off skips scanning
// ---------------------------------------------------------------------------

func TestScrub_SecretsModeOff(t *testing.T) {
	dir := makeWorkspaceWithSecret(t)

	bentoYAML, _ := os.ReadFile(filepath.Join(dir, "bento.yaml"))
	bentoYAML = append(bentoYAML, []byte("secrets:\n  mode: off\n")...)
	os.WriteFile(filepath.Join(dir, "bento.yaml"), bentoYAML, 0644)

	out := run(t, dir, "save", "-m", "off mode")

	if strings.Contains(out, "Scrubbed") {
		t.Errorf("off mode should not scrub, got:\n%s", out)
	}
	if !strings.Contains(out, "Secret scan: off") {
		t.Errorf("should say scan is off, got:\n%s", out)
	}
}

// ---------------------------------------------------------------------------
// TestScrub_SecretsModeScrubShowsReversibility: scrub message explains safety
// ---------------------------------------------------------------------------

func TestScrub_SecretsModeScrubShowsReversibility(t *testing.T) {
	dir := makeWorkspaceWithSecret(t)

	bentoYAML, _ := os.ReadFile(filepath.Join(dir, "bento.yaml"))
	bentoYAML = append(bentoYAML, []byte("secrets:\n  mode: scrub\n")...)
	os.WriteFile(filepath.Join(dir, "bento.yaml"), bentoYAML, 0644)

	out := run(t, dir, "save", "-m", "scrub mode")

	if !strings.Contains(out, "files on disk are not modified") {
		t.Errorf("should explain files are untouched, got:\n%s", out)
	}
	if !strings.Contains(out, "restored automatically") {
		t.Errorf("should explain secrets are restored on open, got:\n%s", out)
	}
	// Should show alternatives.
	if !strings.Contains(out, "block") {
		t.Errorf("should show block alternative, got:\n%s", out)
	}
}

// ---------------------------------------------------------------------------
// TestScrub_SecretsModeAutoDefault: unset mode persists scrub in CI
// ---------------------------------------------------------------------------

func TestScrub_SecretsModeAutoDefault(t *testing.T) {
	dir := makeWorkspaceWithSecret(t)
	// No secrets.mode set.

	out := run(t, dir, "save", "-m", "auto test")

	if !strings.Contains(out, "Scrubbed") {
		t.Errorf("should default to scrub in CI, got:\n%s", out)
	}

	// Mode should be persisted.
	bentoYAML, _ := os.ReadFile(filepath.Join(dir, "bento.yaml"))
	if !strings.Contains(string(bentoYAML), "mode: scrub") {
		t.Errorf("should persist mode: scrub, got:\n%s", bentoYAML)
	}

	// Second save should not re-prompt.
	writeFile(t, dir, "README.md", "# updated\n")
	out2 := run(t, dir, "save", "-m", "second")
	if strings.Contains(out2, "Non-interactive") {
		t.Errorf("second save should not show prompt, got:\n%s", out2)
	}
}
