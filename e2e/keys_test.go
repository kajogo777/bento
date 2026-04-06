//go:build integration

package e2e_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// startRegistry starts a local OCI registry container on a random port.
// Returns the host:port string and a cleanup function.
func startRegistry(t *testing.T) (string, func()) {
	t.Helper()

	name := fmt.Sprintf("bento-e2e-registry-%d", time.Now().UnixNano()%100000)

	// Use -p 0:5000 to let Docker pick a free host port, then read it back.
	out, err := exec.Command("docker", "run", "-d", "--rm",
		"--name", name,
		"-p", "127.0.0.1::5000",
		"registry:2",
	).CombinedOutput()
	if err != nil {
		t.Skipf("cannot start registry container (docker not available?): %v\n%s", err, out)
	}
	containerID := strings.TrimSpace(string(out))

	cleanup := func() {
		_ = exec.Command("docker", "rm", "-f", containerID).Run()
	}

	// Read the assigned host port.
	portOut, portErr := exec.Command("docker", "port", containerID, "5000/tcp").Output()
	if portErr != nil {
		cleanup()
		t.Fatalf("cannot read registry port: %v", portErr)
	}
	// Output format: "0.0.0.0:XXXXX\n" or "127.0.0.1:XXXXX\n"
	addr := strings.TrimSpace(string(portOut))
	// Normalize to localhost.
	if strings.HasPrefix(addr, "0.0.0.0:") {
		addr = "localhost:" + strings.TrimPrefix(addr, "0.0.0.0:")
	} else if strings.HasPrefix(addr, "127.0.0.1:") {
		addr = "localhost:" + strings.TrimPrefix(addr, "127.0.0.1:")
	}

	// Wait for registry to be ready.
	for i := 0; i < 30; i++ {
		cmd := exec.Command("docker", "exec", containerID, "wget", "-q", "-O", "/dev/null", "http://localhost:5000/v2/")
		if cmd.Run() == nil {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}

	return addr, cleanup
}

// makeWorkspaceWithSecretAndStore creates a workspace with a secret and
// an explicit store path (both temp dirs). Returns (workDir, storeDir).
func makeWorkspaceWithSecretAndStore(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	storeDir := t.TempDir()

	bentoYAML := "store: " + storeDir + "\n"
	if err := os.WriteFile(filepath.Join(dir, "bento.yaml"), []byte(bentoYAML), 0644); err != nil {
		t.Fatal(err)
	}

	writeFile(t, dir, "main.go", "package main\n\nfunc main() {}\n")
	writeFile(t, dir, "README.md", "# My Project\n")

	mcpJSON := `{
  "mcpServers": {
    "api": {
      "env": {
        "AWS_KEY": "` + "AKIA3EXAMPLE7KEYTEST" + `"
      }
    }
  }
}
`
	writeFile(t, dir, ".mcp.json", mcpJSON)
	return dir, storeDir
}

// keysDir creates an isolated keys directory for a simulated user.
func keysDir(t *testing.T, label string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), label+"-keys")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	return dir
}

// runWithKeysDir runs bento with BENTO_KEYS_DIR set to an isolated directory.
func runWithKeysDir(t *testing.T, dir string, keysDir string, args ...string) string {
	t.Helper()
	cmd := exec.Command(bento, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "BENTO_KEYS_DIR="+keysDir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("bento %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

// runWithKeysExpectFail runs bento with BENTO_KEYS_DIR and expects failure.
func runWithKeysExpectFail(t *testing.T, dir string, keysDir string, args ...string) string {
	t.Helper()
	cmd := exec.Command(bento, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "BENTO_KEYS_DIR="+keysDir)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("bento %s expected failure but succeeded\n%s", strings.Join(args, " "), out)
	}
	return string(out)
}

// extractPubFromOutput parses a bento-pk-... string from command output.
func extractPubFromOutput(t *testing.T, out string) string {
	t.Helper()
	for _, line := range strings.Split(out, "\n") {
		if idx := strings.Index(line, "bento-pk-"); idx >= 0 {
			return strings.TrimSpace(line[idx:])
		}
	}
	t.Fatalf("no public key found in output:\n%s", out)
	return ""
}

// ---------------------------------------------------------------------------
// TestKeyWrapping_GenerateListPublic: basic key management commands
// ---------------------------------------------------------------------------

func TestKeyWrapping_GenerateListPublic(t *testing.T) {
	kd := t.TempDir()

	// Generate default keypair.
	out := runWithKeysDir(t, ".", kd, "keys", "generate")
	if !strings.Contains(out, "bento-pk-") {
		t.Fatalf("generate should show public key, got:\n%s", out)
	}
	if !strings.Contains(out, "default") {
		t.Errorf("generate should mention 'default', got:\n%s", out)
	}

	// List keypairs.
	listOut := runWithKeysDir(t, ".", kd, "keys", "list")
	if !strings.Contains(listOut, "default") {
		t.Errorf("list should show default keypair, got:\n%s", listOut)
	}
	if !strings.Contains(listOut, "bento-pk-") {
		t.Errorf("list should show public key, got:\n%s", listOut)
	}

	// Duplicate generate should fail.
	failOut := runWithKeysExpectFail(t, ".", kd, "keys", "generate")
	if !strings.Contains(failOut, "already exists") {
		t.Errorf("duplicate generate should say 'already exists', got:\n%s", failOut)
	}

	// Generate named keypair.
	namedOut := runWithKeysDir(t, ".", kd, "keys", "generate", "--name", "work")
	if !strings.Contains(namedOut, "work") {
		t.Errorf("named generate should mention 'work', got:\n%s", namedOut)
	}

	// List should show both.
	listOut2 := runWithKeysDir(t, ".", kd, "keys", "list")
	if !strings.Contains(listOut2, "default") || !strings.Contains(listOut2, "work") {
		t.Errorf("list should show both keypairs, got:\n%s", listOut2)
	}
}

// ---------------------------------------------------------------------------
// TestKeyWrapping_AddRemoveRecipient: recipient management
// ---------------------------------------------------------------------------

func TestKeyWrapping_AddRemoveRecipient(t *testing.T) {
	kd := t.TempDir()

	// Generate a keypair to get a valid public key.
	out := runWithKeysDir(t, ".", kd, "keys", "generate")
	senderPub := extractPubFromOutput(t, out)

	// Create a workspace for recipients management (needs bento.yaml).
	dir := t.TempDir()
	storeDir := t.TempDir()
	bentoYAML := "store: " + storeDir + "\n"
	os.WriteFile(filepath.Join(dir, "bento.yaml"), []byte(bentoYAML), 0644)

	// Add recipient via "bento recipients add".
	run(t, dir, "recipients", "add", "alice", senderPub)

	// List should show alice.
	listOut := run(t, dir, "recipients", "list")
	if !strings.Contains(listOut, "alice") {
		t.Errorf("list should show alice, got:\n%s", listOut)
	}

	// Remove alice.
	run(t, dir, "recipients", "remove", "alice")

	// List should be empty.
	listOut2 := run(t, dir, "recipients", "list")
	if strings.Contains(listOut2, "alice") {
		t.Errorf("alice should be removed, got:\n%s", listOut2)
	}
}


// ---------------------------------------------------------------------------
// TestKeyWrapping_SaveOpenLocalRoundTrip: save with recipients, open on
// same machine (local backend wins — no wrapping needed).
// ---------------------------------------------------------------------------

func TestKeyWrapping_SaveOpenLocalRoundTrip(t *testing.T) {
	dir, _ := makeWorkspaceWithSecretAndStore(t)

	// Save + open locally — should work via envelope + default keypair.
	run(t, dir, "save", "-m", "local round-trip")
	dst := t.TempDir()
	run(t, dir, "open", "cp-1", dst)

	dstContent, err := os.ReadFile(filepath.Join(dst, ".mcp.json"))
	if err != nil {
		t.Fatalf("reading restored .mcp.json: %v", err)
	}
	if strings.Contains(string(dstContent), "__BENTO_SCRUBBED") {
		t.Errorf("should not contain placeholders, got:\n%s", dstContent)
	}
}

// ---------------------------------------------------------------------------
// TestKeyWrapping_PushPullWithDataKey: push + pull with --data-key (existing
// symmetric flow, no key wrapping). Uses a local registry.
// ---------------------------------------------------------------------------

func TestKeyWrapping_PushPullWithDataKey(t *testing.T) {
	registryAddr, cleanup := startRegistry(t)
	defer cleanup()
	dir, _ := makeWorkspaceWithSecretAndStore(t)

	run(t, dir, "save", "-m", "keypair push-pull")

	// Configure remote.
	repoName := fmt.Sprintf("bento-e2e-dk-%d", time.Now().UnixNano()%100000)
	appendToFile(t, dir, "bento.yaml", "remote: "+registryAddr+"/"+repoName+"\n")

	// Push with --include-secrets.
	pushOut := run(t, dir, "push", "--include-secrets")
	if !strings.Contains(pushOut, "Done") {
		t.Fatalf("push should succeed, got:\n%s", pushOut)
	}
	if !strings.Contains(pushOut, "Re-wrapped") {
		t.Errorf("push output should show re-wrap info, got:\n%s", pushOut)
	}

	// Simulate different machine: delete local secrets.
	deleteLocalSecrets(t, dir)

	// Open from registry — keypair auto-discovery from OCI layer.
	remoteRef := registryAddr + "/" + repoName + ":cp-1"
	dst := t.TempDir()
	dstYAML := fmt.Sprintf("store: %s\n", t.TempDir())
	os.WriteFile(filepath.Join(dst, "bento.yaml"), []byte(dstYAML), 0644)

	run(t, dst, "open", remoteRef, dst)

	content, err := os.ReadFile(filepath.Join(dst, ".mcp.json"))
	if err != nil {
		t.Fatalf("reading restored .mcp.json: %v", err)
	}
	if strings.Contains(string(content), "__BENTO_SCRUBBED") {
		t.Errorf("should not contain placeholders, got:\n%s", content)
	}
}

// ---------------------------------------------------------------------------
// TestKeyWrapping_BentoYAMLRecipients: recipients configured in bento.yaml
// should trigger re-wrap at push time without needing --recipient CLI flag.
// This is the "team workflow" where recipients are committed to the project.
// ---------------------------------------------------------------------------

func TestKeyWrapping_BentoYAMLRecipients(t *testing.T) {
	registryAddr, cleanup := startRegistry(t)
	defer cleanup()

	senderKeysDir := keysDir(t, "sender")
	recipientKeysDir := keysDir(t, "recipient")

	// Generate sender and recipient keypairs in isolated dirs.
	runWithKeysDir(t, ".", senderKeysDir, "keys", "generate")
	recipOut := runWithKeysDir(t, ".", recipientKeysDir, "keys", "generate")
	recipPub := extractPubFromOutput(t, recipOut)

	// Create workspace with a secret.
	dir, _ := makeWorkspaceWithSecretAndStore(t)

	// Add recipient to bento.yaml (the team workflow).
	appendToFile(t, dir, "bento.yaml", fmt.Sprintf("recipients:\n  - name: charlie\n    key: %s\n", recipPub))

	// Save (envelope wraps for sender only at save time).
	runWithKeysDir(t, dir, senderKeysDir, "save", "-m", "yaml-recipients test")

	// Configure remote.
	repoName := fmt.Sprintf("bento-e2e-yamlrecip-%d", time.Now().UnixNano()%100000)
	appendToFile(t, dir, "bento.yaml", "remote: "+registryAddr+"/"+repoName+"\n")

	// Push with --include-secrets but NO --recipient flag.
	// bento.yaml recipients should trigger re-wrap automatically.
	pushOut := runWithKeysDir(t, dir, senderKeysDir, "push", "--include-secrets")
	if !strings.Contains(pushOut, "Done") {
		t.Fatalf("push failed:\n%s", pushOut)
	}
	if !strings.Contains(pushOut, "Re-wrapped") {
		t.Errorf("push should re-wrap for bento.yaml recipients, got:\n%s", pushOut)
	}

	// Simulate "different machine": open as recipient using only their key.
	dst := t.TempDir()
	dstYAML := fmt.Sprintf("store: %s\n", t.TempDir())
	os.WriteFile(filepath.Join(dst, "bento.yaml"), []byte(dstYAML), 0644)

	remoteRef := fmt.Sprintf("%s/%s:cp-1", registryAddr, repoName)
	openOut := runWithKeysDir(t, dst, recipientKeysDir, "open", remoteRef, dst)
	if !strings.Contains(openOut, "Hydrated") {
		t.Errorf("open should hydrate via key wrapping, got:\n%s", openOut)
	}

	// Verify file has real secret, no placeholders.
	content, err := os.ReadFile(filepath.Join(dst, ".mcp.json"))
	if err != nil {
		t.Fatalf("reading .mcp.json: %v", err)
	}
	if strings.Contains(string(content), "__BENTO_SCRUBBED") {
		t.Error("should not contain placeholders")
	}
	if !strings.Contains(string(content), "AKIA3EXAMPLE7KEYTEST") {
		t.Errorf("should contain real secret, got:\n%s", content)
	}
}

// ---------------------------------------------------------------------------
// TestKeyWrapping_WrongDataKeyFails: wrong --data-key can't decrypt
// ---------------------------------------------------------------------------

func TestKeyWrapping_WrongDataKeyFails(t *testing.T) {
	registryAddr, cleanup := startRegistry(t)
	defer cleanup()
	dir, _ := makeWorkspaceWithSecretAndStore(t)
	run(t, dir, "save", "-m", "no-key fail test")

	repoName := fmt.Sprintf("bento-e2e-wk-%d", time.Now().UnixNano()%100000)
	appendToFile(t, dir, "bento.yaml", "remote: "+registryAddr+"/"+repoName+"\n")

	// Push WITHOUT --include-secrets (no secrets in OCI).
	run(t, dir, "push")

	// Delete local secrets.
	deleteLocalSecrets(t, dir)

	// Open should fail — no local envelope, no secrets in OCI.
	remoteRef := registryAddr + "/" + repoName + ":cp-1"
	dst := t.TempDir()
	dstYAML := fmt.Sprintf("store: %s\n", t.TempDir())
	os.WriteFile(filepath.Join(dst, "bento.yaml"), []byte(dstYAML), 0644)

	out := runExpectFail(t, dst, "open", remoteRef, dst)
	if !strings.Contains(out, "cannot be resolved") {
		t.Errorf("should fail without secrets, got:\n%s", out)
	}
}

// ---------------------------------------------------------------------------
// TestKeyWrapping_FallbackWithoutDataKey: open without --data-key and no local
// secrets should fail with an actionable error.
// ---------------------------------------------------------------------------

func TestKeyWrapping_FallbackNoKeyFails(t *testing.T) {
	registryAddr, cleanup := startRegistry(t)
	defer cleanup()
	dir, _ := makeWorkspaceWithSecretAndStore(t)
	run(t, dir, "save", "-m", "fallback fail test")

	repoName := fmt.Sprintf("bento-e2e-fb-%d", time.Now().UnixNano()%100000)
	appendToFile(t, dir, "bento.yaml", "remote: "+registryAddr+"/"+repoName+"\n")
	run(t, dir, "push")

	deleteLocalSecrets(t, dir)

	remoteRef := registryAddr + "/" + repoName + ":cp-1"
	dst := t.TempDir()
	dstYAML := fmt.Sprintf("store: %s\n", t.TempDir())
	os.WriteFile(filepath.Join(dst, "bento.yaml"), []byte(dstYAML), 0644)

	out := runExpectFail(t, dst, "open", remoteRef, dst)
	if !strings.Contains(out, "cannot be resolved") {
		t.Errorf("error should mention resolution failure, got:\n%s", out)
	}
}

// ---------------------------------------------------------------------------
// TestKeyWrapping_DataKeyViaEnvVar: BENTO_DATA_KEY env var works for decryption
// ---------------------------------------------------------------------------

func TestKeyWrapping_DataKeyViaEnvVar(t *testing.T) {
	dir, _ := makeWorkspaceWithSecretAndStore(t)
	run(t, dir, "save", "-m", "env var test")

	// Same-machine open works without any env vars or flags.
	dst := t.TempDir()
	run(t, dir, "open", "cp-1", dst)

	content, err := os.ReadFile(filepath.Join(dst, ".mcp.json"))
	if err != nil {
		t.Fatalf("reading restored .mcp.json: %v", err)
	}
	if strings.Contains(string(content), "__BENTO_SCRUBBED") {
		t.Errorf("should hydrate via local envelope, got:\n%s", content)
	}
}

// ---------------------------------------------------------------------------
// TestKeyWrapping_SecretsExport: bento secrets export emits ciphertext on
// stdout and data key on stderr
// ---------------------------------------------------------------------------

func TestKeyWrapping_SecretsExport(t *testing.T) {
	dir, _ := makeWorkspaceWithSecretAndStore(t)
	run(t, dir, "save", "-m", "export test")

	cmd := exec.Command(bento, "secrets", "export", "cp-1")
	cmd.Dir = dir
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("export failed: %v\nstderr: %s", err, stderr.String())
	}

	// Stdout should have the envelope.
	if len(stdout.String()) == 0 {
		t.Error("stdout should contain envelope")
	}
	// Should be encrypted, not readable.
	if strings.Contains(stdout.String(), "AKIA") {
		t.Error("stdout should be encrypted, not plaintext")
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func appendToFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	data, _ := os.ReadFile(path)
	data = append(data, []byte(content)...)
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
}


// ---------------------------------------------------------------------------
// TestKeyWrapping_FullCycleWithRecipients: generate keypair → save with
// --recipient → push --include-secrets → open on "different machine" with
// recipient's private key → verify secrets hydrated via auto-discovery.
// ---------------------------------------------------------------------------

func TestKeyWrapping_FullCycleWithRecipients(t *testing.T) {
	registryAddr, cleanup := startRegistry(t)
	defer cleanup()

	senderKeys := t.TempDir()
	recipientKeys := t.TempDir()

	// Generate sender and recipient keypairs in isolated dirs.
	senderOut := runWithKeysDir(t, ".", senderKeys, "keys", "generate")
	recipOut := runWithKeysDir(t, ".", recipientKeys, "keys", "generate")

	recipPub := extractPubFromOutput(t, recipOut)
	_ = extractPubFromOutput(t, senderOut) // validate it parsed

	// Save with --recipient pointing to the recipient's public key.
	dir, _ := makeWorkspaceWithSecretAndStore(t)

	saveOut := runWithKeysDir(t, dir, senderKeys, "save", "-m", "wrapped save")
	if !strings.Contains(saveOut, "cp-1") {
		t.Errorf("save should create cp-1, got:\n%s", saveOut)
	}

	// Configure remote and push.
	repoName := fmt.Sprintf("bento-e2e-wrap-%d", time.Now().UnixNano()%100000)
	appendToFile(t, dir, "bento.yaml", "remote: "+registryAddr+"/"+repoName+"\n")
	pushOut := runWithKeysDir(t, dir, senderKeys, "push", "--include-secrets", "--recipient", recipPub)
	if !strings.Contains(pushOut, "Done") {
		t.Fatalf("push failed:\n%s", pushOut)
	}
	if !strings.Contains(pushOut, "Re-wrapped") {
		t.Errorf("push should show re-wrap info, got:\n%s", pushOut)
	}
	if !strings.Contains(pushOut, "auto-decrypts") {
		t.Errorf("wrapped push should mention auto-decrypt, got:\n%s", pushOut)
	}

	// Simulate "different machine": delete local secrets, open as recipient.
	deleteLocalSecrets(t, dir)

	dst := t.TempDir()
	dstBentoYAML := fmt.Sprintf("store: %s\n", t.TempDir())
	os.WriteFile(filepath.Join(dst, "bento.yaml"), []byte(dstBentoYAML), 0644)

	remoteRef := fmt.Sprintf("%s/%s:cp-1", registryAddr, repoName)
	openOut := runWithKeysDir(t, dst, recipientKeys, "open", remoteRef, dst)
	if !strings.Contains(openOut, "Hydrated") {
		t.Errorf("open should hydrate via key wrapping, got:\n%s", openOut)
	}
	if !strings.Contains(openOut, "keypair auto-discovery") {
		t.Errorf("open should mention keypair auto-discovery, got:\n%s", openOut)
	}

	// Verify file has real secret.
	content, err := os.ReadFile(filepath.Join(dst, ".mcp.json"))
	if err != nil {
		t.Fatalf("reading .mcp.json: %v", err)
	}
	if strings.Contains(string(content), "__BENTO_SCRUBBED") {
		t.Error("should not contain placeholders")
	}
	if !strings.Contains(string(content), "AKIA3EXAMPLE7KEYTEST") {
		t.Errorf("should contain real secret, got:\n%s", content)
	}
}

// ---------------------------------------------------------------------------
// TestKeyWrapping_MultiRecipient: wrap to 2 recipients, each can open
// independently, non-recipient cannot.
// ---------------------------------------------------------------------------

func TestKeyWrapping_MultiRecipient(t *testing.T) {
	registryAddr, cleanup := startRegistry(t)
	defer cleanup()
	dir, keysDir := makeWorkspaceWithSecretAndStore(t)

	// Generate two recipient keypairs.
	runWithKeysDir(t, dir, keysDir, "keys", "generate", "--name", "alice")
	runWithKeysDir(t, dir, keysDir, "keys", "generate", "--name", "bob")

	// Get alice and bob public keys.
	listOut := runWithKeysDir(t, dir, keysDir, "keys", "list")
	t.Logf("keys list:\n%s", listOut)

	runWithKeysDir(t, dir, keysDir, "save", "-m", "multi-recip test")

	repoName := fmt.Sprintf("bento-e2e-mr-%d", time.Now().UnixNano()%100000)
	appendToFile(t, dir, "bento.yaml", "remote: "+registryAddr+"/"+repoName+"\n")

	// Push with --include-secrets.
	runWithKeysDir(t, dir, keysDir, "push", "--include-secrets")

	// Open from registry — should work via keypair auto-discovery.
	deleteLocalSecrets(t, dir)
	remoteRef := registryAddr + "/" + repoName + ":cp-1"
	dst := t.TempDir()
	dstYAML := fmt.Sprintf("store: %s\n", t.TempDir())
	os.WriteFile(filepath.Join(dst, "bento.yaml"), []byte(dstYAML), 0644)

	// Copy keys so the "other machine" has them for auto-discovery.
	dstKeysDir := filepath.Join(dst, ".bento-keys")
	exec.Command("cp", "-r", keysDir, dstKeysDir).Run()

	runWithEnv(t, dst, map[string]string{"BENTO_KEYS_DIR": keysDir}, "open", remoteRef, dst)

	content, err := os.ReadFile(filepath.Join(dst, ".mcp.json"))
	if err != nil {
		t.Fatalf("reading restored .mcp.json: %v", err)
	}
	if strings.Contains(string(content), "__BENTO_SCRUBBED") {
		t.Errorf("should not contain placeholders, got:\n%s", content)
	}
}

// ---------------------------------------------------------------------------
// TestKeyWrapping_PushTimeRecipient: save without recipients (symmetric),
// then push --include-secrets --recipient to wrap at push time.
// ---------------------------------------------------------------------------

func TestKeyWrapping_PushTimeRecipient(t *testing.T) {
	registryAddr, cleanup := startRegistry(t)
	defer cleanup()
	dir, keysDir := makeWorkspaceWithSecretAndStore(t)

	// Generate a recipient keypair.
	runWithKeysDir(t, dir, keysDir, "keys", "generate", "--name", "charlie")

	runWithKeysDir(t, dir, keysDir, "save", "-m", "push-time recipient test")

	repoName := fmt.Sprintf("bento-e2e-ptr-%d", time.Now().UnixNano()%100000)
	appendToFile(t, dir, "bento.yaml", "remote: "+registryAddr+"/"+repoName+"\n")

	// Get charlie's public key.
	listOut := runWithKeysDir(t, dir, keysDir, "keys", "list")
	var charliePub string
	for _, line := range strings.Split(listOut, "\n") {
		if strings.Contains(line, "charlie") {
			fields := strings.Fields(line)
			for _, f := range fields {
				if strings.HasPrefix(f, "bento-pk-") {
					charliePub = f
					break
				}
			}
		}
	}
	if charliePub == "" {
		t.Fatal("could not find charlie public key")
	}

	// Push with --include-secrets --recipient charlie.
	pushOut := runWithKeysDir(t, dir, keysDir, "push", "--include-secrets", "--recipient", charliePub)
	if !strings.Contains(pushOut, "Re-wrapped") {
		t.Errorf("push should re-wrap, got:\n%s", pushOut)
	}

	// Verify charlie can open.
	deleteLocalSecrets(t, dir)
	remoteRef := registryAddr + "/" + repoName + ":cp-1"
	dst := t.TempDir()
	dstYAML := fmt.Sprintf("store: %s\n", t.TempDir())
	os.WriteFile(filepath.Join(dst, "bento.yaml"), []byte(dstYAML), 0644)

	runWithEnv(t, dst, map[string]string{"BENTO_KEYS_DIR": keysDir}, "open", remoteRef, dst)

	content, err := os.ReadFile(filepath.Join(dst, ".mcp.json"))
	if err != nil {
		t.Fatalf("reading restored .mcp.json: %v", err)
	}
	if strings.Contains(string(content), "__BENTO_SCRUBBED") {
		t.Errorf("should not contain placeholders, got:\n%s", content)
	}
}
