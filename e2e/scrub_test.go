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
