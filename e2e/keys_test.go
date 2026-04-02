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

	// Show public key.
	pubOut := strings.TrimSpace(runWithKeysDir(t, ".", kd, "keys", "public"))
	if !strings.HasPrefix(pubOut, "bento-pk-") {
		t.Errorf("public should print bento-pk-..., got: %q", pubOut)
	}
	if len(pubOut) != len("bento-pk-")+43 {
		t.Errorf("public key wrong length: %d", len(pubOut))
	}

	// Duplicate generate should fail.
	failOut := runWithKeysExpectFail(t, ".", kd, "keys", "generate")
	if !strings.Contains(failOut, "already exists") {
		t.Errorf("duplicate generate should say 'already exists', got:\n%s", failOut)
	}

	// Generate named keypair.
	out2 := runWithKeysDir(t, ".", kd, "keys", "generate", "--name", "work")
	if !strings.Contains(out2, "work") {
		t.Errorf("named generate should mention 'work', got:\n%s", out2)
	}

	// List should now show both.
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

	// Add recipient.
	runWithKeysDir(t, ".", kd, "keys", "add-recipient", "alice", senderPub)

	// List should show alice.
	listOut := runWithKeysDir(t, ".", kd, "keys", "list")
	if !strings.Contains(listOut, "alice") {
		t.Errorf("list should show alice, got:\n%s", listOut)
	}

	// Remove alice.
	runWithKeysDir(t, ".", kd, "keys", "remove-recipient", "alice")

	// Invalid key should fail.
	failOut := runWithKeysExpectFail(t, ".", kd, "keys", "add-recipient", "bob", "not-a-key")
	if !strings.Contains(failOut, "bento-pk-") {
		t.Errorf("should mention expected prefix, got:\n%s", failOut)
	}

	// Remove nonexistent should fail.
	failOut2 := runWithKeysExpectFail(t, ".", kd, "keys", "remove-recipient", "nobody")
	if !strings.Contains(failOut2, "not found") {
		t.Errorf("should say not found, got:\n%s", failOut2)
	}
}


// ---------------------------------------------------------------------------
// TestKeyWrapping_SaveOpenLocalRoundTrip: save with recipients, open on
// same machine (local backend wins — no wrapping needed).
// ---------------------------------------------------------------------------

func TestKeyWrapping_SaveOpenLocalRoundTrip(t *testing.T) {
	dir, _ := makeWorkspaceWithSecretAndStore(t)

	// Save with no recipients — standard flow.
	run(t, dir, "save", "-m", "local round-trip")

	// Open on the same machine — local backend should work.
	dst := t.TempDir()
	out := run(t, dir, "open", "cp-1", dst)
	if !strings.Contains(out, "Hydrated") {
		t.Errorf("open should hydrate secrets, got:\n%s", out)
	}

	// Verify file content.
	content, err := os.ReadFile(filepath.Join(dst, ".mcp.json"))
	if err != nil {
		t.Fatalf("reading .mcp.json: %v", err)
	}
	if strings.Contains(string(content), "__BENTO_SCRUBBED") {
		t.Error("file should not contain placeholders")
	}
	if !strings.Contains(string(content), "AKIA3EXAMPLE7KEYTEST") {
		t.Error("file should contain the original secret value")
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

	// Save.
	run(t, dir, "save", "-m", "data-key push-pull")

	dataKey := extractDataKey(t, dir)
	if dataKey == "" {
		t.Fatal("no data key found after save")
	}
	if !strings.HasPrefix(dataKey, "bento-dk-") {
		t.Errorf("data key should start with bento-dk-, got: %s", dataKey)
	}

	// Configure remote.
	repoName := fmt.Sprintf("bento-e2e-dk-%d", time.Now().UnixNano()%100000)
	appendToFile(t, dir, "bento.yaml", "remote: "+registryAddr+"/"+repoName+"\n")

	// Push with --include-secrets.
	pushOut := run(t, dir, "push", "--include-secrets")
	if !strings.Contains(pushOut, "Done") {
		t.Fatalf("push should succeed, got:\n%s", pushOut)
	}
	if !strings.Contains(pushOut, "bento-dk-") {
		t.Errorf("push output should show data key, got:\n%s", pushOut)
	}

	// Simulate different machine: delete local secrets.
	deleteLocalSecrets(t, dir)

	// Open from registry with --data-key.
	dst := t.TempDir()
	dstBentoYAML := fmt.Sprintf("store: %s\nremote: %s/%s\n", t.TempDir(), registryAddr, repoName)
	os.WriteFile(filepath.Join(dst, "bento.yaml"), []byte(dstBentoYAML), 0644)

	remoteRef := fmt.Sprintf("%s/%s:cp-1", registryAddr, repoName)
	openOut := run(t, dst, "open", remoteRef, dst, "--data-key", dataKey)
	if !strings.Contains(openOut, "Hydrated") {
		t.Errorf("open should hydrate from OCI layer, got:\n%s", openOut)
	}

	content, _ := os.ReadFile(filepath.Join(dst, ".mcp.json"))
	if strings.Contains(string(content), "__BENTO_SCRUBBED") {
		t.Error("should not contain placeholders")
	}
}

// ---------------------------------------------------------------------------
// TestKeyWrapping_WrongDataKeyFails: wrong --data-key can't decrypt
// ---------------------------------------------------------------------------

func TestKeyWrapping_WrongDataKeyFails(t *testing.T) {
	registryAddr, cleanup := startRegistry(t)
	defer cleanup()

	dir, _ := makeWorkspaceWithSecretAndStore(t)
	run(t, dir, "save", "-m", "wrong-key test")

	repoName := fmt.Sprintf("bento-e2e-wrongdk-%d", time.Now().UnixNano()%100000)
	appendToFile(t, dir, "bento.yaml", "remote: "+registryAddr+"/"+repoName+"\n")
	run(t, dir, "push", "--include-secrets")
	deleteLocalSecrets(t, dir)

	wrongKey := "bento-dk-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	dst := t.TempDir()
	dstBentoYAML := fmt.Sprintf("store: %s\n", t.TempDir())
	os.WriteFile(filepath.Join(dst, "bento.yaml"), []byte(dstBentoYAML), 0644)

	remoteRef := fmt.Sprintf("%s/%s:cp-1", registryAddr, repoName)
	out := runExpectFail(t, dst, "open", remoteRef, dst, "--data-key", wrongKey)
	if !strings.Contains(out, "cannot be resolved") && !strings.Contains(out, "decryption failed") {
		t.Errorf("should fail with wrong key, got:\n%s", out)
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
	run(t, dir, "save", "-m", "no-key test")

	repoName := fmt.Sprintf("bento-e2e-nokey-%d", time.Now().UnixNano()%100000)
	appendToFile(t, dir, "bento.yaml", "remote: "+registryAddr+"/"+repoName+"\n")
	run(t, dir, "push", "--include-secrets")
	deleteLocalSecrets(t, dir)

	dst := t.TempDir()
	dstBentoYAML := fmt.Sprintf("store: %s\n", t.TempDir())
	os.WriteFile(filepath.Join(dst, "bento.yaml"), []byte(dstBentoYAML), 0644)

	remoteRef := fmt.Sprintf("%s/%s:cp-1", registryAddr, repoName)
	out := runExpectFail(t, dst, "open", remoteRef, dst)
	if !strings.Contains(out, "--data-key") {
		t.Errorf("error should mention --data-key, got:\n%s", out)
	}
}

// ---------------------------------------------------------------------------
// TestKeyWrapping_DataKeyViaEnvVar: BENTO_DATA_KEY env var works for decryption
// ---------------------------------------------------------------------------

func TestKeyWrapping_DataKeyViaEnvVar(t *testing.T) {
	registryAddr, cleanup := startRegistry(t)
	defer cleanup()

	dir, _ := makeWorkspaceWithSecretAndStore(t)
	run(t, dir, "save", "-m", "env-var test")

	dataKey := extractDataKey(t, dir)
	if dataKey == "" {
		t.Fatal("no data key found")
	}

	repoName := fmt.Sprintf("bento-e2e-envdk-%d", time.Now().UnixNano()%100000)
	appendToFile(t, dir, "bento.yaml", "remote: "+registryAddr+"/"+repoName+"\n")
	run(t, dir, "push", "--include-secrets")
	deleteLocalSecrets(t, dir)

	dst := t.TempDir()
	dstBentoYAML := fmt.Sprintf("store: %s\n", t.TempDir())
	os.WriteFile(filepath.Join(dst, "bento.yaml"), []byte(dstBentoYAML), 0644)

	remoteRef := fmt.Sprintf("%s/%s:cp-1", registryAddr, repoName)
	out := runWithEnv(t, dst, map[string]string{"BENTO_DATA_KEY": dataKey},
		"open", remoteRef, dst)
	if !strings.Contains(out, "Hydrated") {
		t.Errorf("BENTO_DATA_KEY should decrypt, got:\n%s", out)
	}

	content, _ := os.ReadFile(filepath.Join(dst, ".mcp.json"))
	if strings.Contains(string(content), "__BENTO_SCRUBBED") {
		t.Error("should not contain placeholders")
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
		t.Fatalf("export failed: %v", err)
	}

	// Stderr should have the data key.
	if !strings.Contains(stderr.String(), "bento-dk-") {
		t.Errorf("stderr should contain data key, got:\n%s", stderr.String())
	}
	if !strings.Contains(stderr.String(), "--data-key") {
		t.Errorf("stderr should mention --data-key flag, got:\n%s", stderr.String())
	}

	// Stdout should have ciphertext only (not the key).
	if strings.Contains(stdout.String(), "bento-dk-") {
		t.Error("stdout should NOT contain the data key")
	}
	if len(stdout.String()) == 0 {
		t.Error("stdout should contain ciphertext")
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

	saveOut := runWithKeysDir(t, dir, senderKeys, "save", "-m", "wrapped save", "--recipient", recipPub)
	if !strings.Contains(saveOut, "Wrapped secrets") {
		t.Errorf("save should report wrapped secrets, got:\n%s", saveOut)
	}
	if !strings.Contains(saveOut, "Sealed by:") {
		t.Errorf("save should report sealed-by, got:\n%s", saveOut)
	}

	// Configure remote and push.
	repoName := fmt.Sprintf("bento-e2e-wrap-%d", time.Now().UnixNano()%100000)
	appendToFile(t, dir, "bento.yaml", "remote: "+registryAddr+"/"+repoName+"\n")
	pushOut := runWithKeysDir(t, dir, senderKeys, "push", "--include-secrets")
	if !strings.Contains(pushOut, "Done") {
		t.Fatalf("push failed:\n%s", pushOut)
	}
	// Key-wrapped push should NOT show a bento-dk- data key.
	if strings.Contains(pushOut, "bento-dk-") {
		t.Errorf("wrapped push should NOT show data key, got:\n%s", pushOut)
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

	senderKeys := t.TempDir()
	aliceKeys := t.TempDir()
	bobKeys := t.TempDir()
	outsiderKeys := t.TempDir()

	runWithKeysDir(t, ".", senderKeys, "keys", "generate")
	aliceOut := runWithKeysDir(t, ".", aliceKeys, "keys", "generate")
	bobOut := runWithKeysDir(t, ".", bobKeys, "keys", "generate")
	runWithKeysDir(t, ".", outsiderKeys, "keys", "generate")

	alicePub := extractPubFromOutput(t, aliceOut)
	bobPub := extractPubFromOutput(t, bobOut)

	// Save with both recipients.
	dir, _ := makeWorkspaceWithSecretAndStore(t)
	runWithKeysDir(t, dir, senderKeys, "save", "-m", "multi-recip", "--recipient", alicePub, "--recipient", bobPub)

	repoName := fmt.Sprintf("bento-e2e-multi-%d", time.Now().UnixNano()%100000)
	appendToFile(t, dir, "bento.yaml", "remote: "+registryAddr+"/"+repoName+"\n")
	runWithKeysDir(t, dir, senderKeys, "push", "--include-secrets")
	deleteLocalSecrets(t, dir)
	remoteRef := fmt.Sprintf("%s/%s:cp-1", registryAddr, repoName)

	tryOpenAs := func(kd string, label string, expectSuccess bool) {
		dst := t.TempDir()
		dstYAML := fmt.Sprintf("store: %s\n", t.TempDir())
		os.WriteFile(filepath.Join(dst, "bento.yaml"), []byte(dstYAML), 0644)

		if expectSuccess {
			out := runWithKeysDir(t, dst, kd, "open", remoteRef, dst)
			if !strings.Contains(out, "Hydrated") {
				t.Errorf("%s should be able to open, got:\n%s", label, out)
			}
		} else {
			out := runWithKeysExpectFail(t, dst, kd, "open", remoteRef, dst)
			if !strings.Contains(out, "cannot be resolved") && !strings.Contains(out, "--data-key") {
				t.Errorf("%s should fail to open, got:\n%s", label, out)
			}
		}
	}

	tryOpenAs(aliceKeys, "alice", true)
	tryOpenAs(bobKeys, "bob", true)
	tryOpenAs(senderKeys, "sender (implicit recipient)", true)
	tryOpenAs(outsiderKeys, "outsider", false)
}

// ---------------------------------------------------------------------------
// TestKeyWrapping_PushTimeRecipient: save without recipients (symmetric),
// then push --include-secrets --recipient to wrap at push time.
// ---------------------------------------------------------------------------

func TestKeyWrapping_PushTimeRecipient(t *testing.T) {
	registryAddr, cleanup := startRegistry(t)
	defer cleanup()

	senderKeys := t.TempDir()
	recipientKeys := t.TempDir()

	runWithKeysDir(t, ".", senderKeys, "keys", "generate")
	recipOut := runWithKeysDir(t, ".", recipientKeys, "keys", "generate")
	recipPub := extractPubFromOutput(t, recipOut)

	// Save WITHOUT recipients — produces old-format envelope.
	dir, _ := makeWorkspaceWithSecretAndStore(t)
	runWithKeysDir(t, dir, senderKeys, "save", "-m", "push-wrap")

	dataKey := extractDataKey(t, dir)
	if dataKey == "" {
		t.Fatal("no data key found")
	}

	// Push WITH --recipient — wraps at push time.
	repoName := fmt.Sprintf("bento-e2e-pushwrap-%d", time.Now().UnixNano()%100000)
	appendToFile(t, dir, "bento.yaml", "remote: "+registryAddr+"/"+repoName+"\n")
	pushOut := runWithKeysDir(t, dir, senderKeys, "push", "--include-secrets", "--recipient", recipPub)
	if !strings.Contains(pushOut, "Done") {
		t.Fatalf("push failed:\n%s", pushOut)
	}
	if strings.Contains(pushOut, "bento-dk-") {
		t.Errorf("push with --recipient should NOT show data key, got:\n%s", pushOut)
	}
	if !strings.Contains(pushOut, "auto-decrypts") {
		t.Errorf("should mention auto-decrypt, got:\n%s", pushOut)
	}

	// Recipient can open via auto-discovery.
	deleteLocalSecrets(t, dir)
	dst := t.TempDir()
	os.WriteFile(filepath.Join(dst, "bento.yaml"), []byte(fmt.Sprintf("store: %s\n", t.TempDir())), 0644)
	remoteRef := fmt.Sprintf("%s/%s:cp-1", registryAddr, repoName)
	openOut := runWithKeysDir(t, dst, recipientKeys, "open", remoteRef, dst)
	if !strings.Contains(openOut, "Hydrated") {
		t.Errorf("recipient should be able to open, got:\n%s", openOut)
	}

	content, _ := os.ReadFile(filepath.Join(dst, ".mcp.json"))
	if strings.Contains(string(content), "__BENTO_SCRUBBED") {
		t.Error("should not contain placeholders")
	}
}
