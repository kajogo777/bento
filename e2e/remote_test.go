//go:build integration

package e2e_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// TestPushPersistsRemote: pushing with a URL arg saves it to bento.yaml
// ---------------------------------------------------------------------------

func TestPushPersistsRemote(t *testing.T) {
	registryAddr, cleanup := startRegistry(t)
	defer cleanup()

	dir := makeWorkspace(t)
	run(t, dir, "save", "--skip-secret-scan", "-m", "first")

	repoName := fmt.Sprintf("bento-e2e-push-remote-%d", time.Now().UnixNano()%100000)
	remote := registryAddr + "/" + repoName

	out := run(t, dir, "push", remote)
	if !strings.Contains(out, "Remote: "+remote) {
		t.Errorf("push should print persisted remote, got:\n%s", out)
	}

	// Verify bento.yaml has the remote.
	data, err := os.ReadFile(filepath.Join(dir, "bento.yaml"))
	if err != nil {
		t.Fatalf("reading bento.yaml: %v", err)
	}
	if !strings.Contains(string(data), "remote: "+remote) {
		t.Errorf("bento.yaml should contain remote, got:\n%s", data)
	}

	// Second push should NOT print "Remote:" again (already set).
	out2 := run(t, dir, "push")
	if strings.Contains(out2, "Remote:") {
		t.Errorf("second push should not re-print Remote, got:\n%s", out2)
	}
}

// ---------------------------------------------------------------------------
// TestOpenRemotePersistsRemote: opening from a registry ref saves remote
// ---------------------------------------------------------------------------

func TestOpenRemotePersistsRemote(t *testing.T) {
	registryAddr, cleanup := startRegistry(t)
	defer cleanup()

	// Source workspace: save and push.
	dir := makeWorkspace(t)
	run(t, dir, "save", "--skip-secret-scan", "-m", "source")

	repoName := fmt.Sprintf("bento-e2e-open-remote-%d", time.Now().UnixNano()%100000)
	remote := registryAddr + "/" + repoName
	run(t, dir, "push", remote)

	// Open into fresh directory (cold start).
	dst := t.TempDir()
	dstStore := t.TempDir()
	// Pre-create bento.yaml with only store so we control the store path.
	os.WriteFile(filepath.Join(dst, "bento.yaml"), []byte("store: "+dstStore+"\n"), 0644)

	out := run(t, dst, "open", remote+":latest", dst)
	if !strings.Contains(out, "Remote: "+remote) {
		t.Errorf("open should print persisted remote, got:\n%s", out)
	}

	// Verify bento.yaml has the remote.
	data, err := os.ReadFile(filepath.Join(dst, "bento.yaml"))
	if err != nil {
		t.Fatalf("reading bento.yaml: %v", err)
	}
	if !strings.Contains(string(data), "remote: "+remote) {
		t.Errorf("destination bento.yaml should contain remote, got:\n%s", data)
	}

	// Push from destination should work without specifying URL.
	writeFile(t, dst, "extra.txt", "new file\n")
	run(t, dst, "save", "--skip-secret-scan", "-m", "from destination")
	pushOut := run(t, dst, "push")
	if !strings.Contains(pushOut, "Done") {
		t.Errorf("push from destination should succeed, got:\n%s", pushOut)
	}
}

// ---------------------------------------------------------------------------
// TestOpenColdStartGeneratesRemote: open from registry into empty dir
// generates bento.yaml with remote set
// ---------------------------------------------------------------------------

func TestOpenColdStartGeneratesRemote(t *testing.T) {
	registryAddr, cleanup := startRegistry(t)
	defer cleanup()

	// Source: save and push.
	dir := makeWorkspace(t)
	run(t, dir, "save", "--skip-secret-scan", "-m", "source")

	repoName := fmt.Sprintf("bento-e2e-cold-%d", time.Now().UnixNano()%100000)
	remote := registryAddr + "/" + repoName
	run(t, dir, "push", remote)

	// Open into completely empty directory (no bento.yaml).
	dst := t.TempDir()
	out := run(t, dst, "open", remote+":latest", dst)

	if !strings.Contains(out, "Generated bento.yaml") {
		t.Errorf("should generate bento.yaml, got:\n%s", out)
	}
	if !strings.Contains(out, "Remote: "+remote) {
		t.Errorf("should persist remote, got:\n%s", out)
	}

	// Verify files were restored.
	if _, err := os.Stat(filepath.Join(dst, "main.go")); os.IsNotExist(err) {
		t.Error("main.go should be restored")
	}

	// Verify remote in generated bento.yaml.
	data, _ := os.ReadFile(filepath.Join(dst, "bento.yaml"))
	if !strings.Contains(string(data), "remote: "+remote) {
		t.Errorf("generated bento.yaml should contain remote, got:\n%s", data)
	}
}

// ---------------------------------------------------------------------------
// TestPull: pull syncs checkpoints from remote without restoring
// ---------------------------------------------------------------------------

func TestPull(t *testing.T) {
	registryAddr, cleanup := startRegistry(t)
	defer cleanup()

	// Source workspace: save two checkpoints and push.
	dir := makeWorkspace(t)
	run(t, dir, "save", "--skip-secret-scan", "-m", "first")
	writeFile(t, dir, "extra.txt", "extra\n")
	run(t, dir, "save", "--skip-secret-scan", "-m", "second")

	repoName := fmt.Sprintf("bento-e2e-pull-%d", time.Now().UnixNano()%100000)
	remote := registryAddr + "/" + repoName
	run(t, dir, "push", remote)

	// Fresh workspace: open cp-1, then pull to get cp-2 without restoring.
	dst := t.TempDir()
	dstStore := t.TempDir()
	os.WriteFile(filepath.Join(dst, "bento.yaml"), []byte("store: "+dstStore+"\n"), 0644)

	run(t, dst, "open", remote+":cp-1", dst)

	// Verify only cp-1 files are on disk (no extra.txt).
	if _, err := os.Stat(filepath.Join(dst, "extra.txt")); !os.IsNotExist(err) {
		t.Error("extra.txt should NOT be on disk after opening cp-1")
	}

	// Pull — should sync cp-2 and latest into local store.
	pullOut := run(t, dst, "pull")
	if !strings.Contains(pullOut, "pulled") {
		t.Errorf("pull should show pulled tags, got:\n%s", pullOut)
	}
	if !strings.Contains(pullOut, "Done") {
		t.Errorf("pull should complete, got:\n%s", pullOut)
	}

	// extra.txt should still NOT be on disk (pull doesn't restore).
	if _, err := os.Stat(filepath.Join(dst, "extra.txt")); !os.IsNotExist(err) {
		t.Error("extra.txt should NOT be on disk after pull (pull doesn't restore)")
	}

	// List should show both checkpoints now.
	listOut := run(t, dst, "list")
	if !strings.Contains(listOut, "cp-1") || !strings.Contains(listOut, "cp-2") {
		t.Errorf("list should show both checkpoints after pull, got:\n%s", listOut)
	}

	// Now open latest — should restore extra.txt.
	run(t, dst, "open", "latest")
	data, err := os.ReadFile(filepath.Join(dst, "extra.txt"))
	if err != nil {
		t.Fatalf("extra.txt should be on disk after open latest: %v", err)
	}
	if string(data) != "extra\n" {
		t.Errorf("extra.txt content mismatch: %q", data)
	}
}

// ---------------------------------------------------------------------------
// TestPullWithTag: pull a specific tag
// ---------------------------------------------------------------------------

func TestPullWithTag(t *testing.T) {
	registryAddr, cleanup := startRegistry(t)
	defer cleanup()

	dir := makeWorkspace(t)
	run(t, dir, "save", "--skip-secret-scan", "-m", "first")
	writeFile(t, dir, "v2.txt", "v2\n")
	run(t, dir, "save", "--skip-secret-scan", "-m", "second")

	repoName := fmt.Sprintf("bento-e2e-pull-tag-%d", time.Now().UnixNano()%100000)
	remote := registryAddr + "/" + repoName
	run(t, dir, "push", remote)

	// Fresh workspace with remote configured.
	dst := t.TempDir()
	dstStore := t.TempDir()
	bentoYAML := "store: " + dstStore + "\nremote: " + remote + "\n"
	os.WriteFile(filepath.Join(dst, "bento.yaml"), []byte(bentoYAML), 0644)

	// Pull only cp-1.
	pullOut := run(t, dst, "pull", "--tag", "cp-1")
	if !strings.Contains(pullOut, "pulled cp-1") {
		t.Errorf("pull should show cp-1, got:\n%s", pullOut)
	}

	// List should show cp-1 but not cp-2.
	listOut := run(t, dst, "list")
	if !strings.Contains(listOut, "cp-1") {
		t.Errorf("list should show cp-1, got:\n%s", listOut)
	}
	if strings.Contains(listOut, "cp-2") {
		t.Errorf("list should NOT show cp-2 (only pulled cp-1), got:\n%s", listOut)
	}
}

// ---------------------------------------------------------------------------
// TestPullPersistsRemote: pulling with a URL arg saves it to bento.yaml
// ---------------------------------------------------------------------------

func TestPullPersistsRemote(t *testing.T) {
	registryAddr, cleanup := startRegistry(t)
	defer cleanup()

	dir := makeWorkspace(t)
	run(t, dir, "save", "--skip-secret-scan", "-m", "first")

	repoName := fmt.Sprintf("bento-e2e-pull-persist-%d", time.Now().UnixNano()%100000)
	remote := registryAddr + "/" + repoName
	run(t, dir, "push", remote)

	// Fresh workspace with no remote.
	dst := t.TempDir()
	dstStore := t.TempDir()
	os.WriteFile(filepath.Join(dst, "bento.yaml"), []byte("store: "+dstStore+"\n"), 0644)

	// Pull with explicit remote URL.
	pullOut := run(t, dst, "pull", remote)
	if !strings.Contains(pullOut, "Remote: "+remote) {
		t.Errorf("pull should persist remote, got:\n%s", pullOut)
	}

	// Verify bento.yaml has remote.
	data, _ := os.ReadFile(filepath.Join(dst, "bento.yaml"))
	if !strings.Contains(string(data), "remote: "+remote) {
		t.Errorf("bento.yaml should have remote after pull, got:\n%s", data)
	}

	// Second pull should not re-print Remote.
	pullOut2 := run(t, dst, "pull")
	if strings.Contains(pullOut2, "Remote:") {
		t.Errorf("second pull should not re-print Remote, got:\n%s", pullOut2)
	}
}

// ---------------------------------------------------------------------------
// TestPullNoRemote: pull without remote configured should fail
// ---------------------------------------------------------------------------

func TestPullNoRemote(t *testing.T) {
	dir := makeWorkspace(t)
	out := runExpectFail(t, dir, "pull")
	if !strings.Contains(out, "no remote configured") {
		t.Errorf("pull without remote should fail with clear message, got:\n%s", out)
	}
}

// ---------------------------------------------------------------------------
// TestStatus: status shows workspace info, head, changes, and remote state
// ---------------------------------------------------------------------------

func TestStatus(t *testing.T) {
	dir := makeWorkspace(t)

	// Status before any save.
	out := run(t, dir, "status")
	if !strings.Contains(out, "Head:") && !strings.Contains(out, "none") {
		t.Errorf("status should show no head before save, got:\n%s", out)
	}

	// Save and check status.
	run(t, dir, "save", "--skip-secret-scan", "-m", "first save")
	out = run(t, dir, "status")
	if !strings.Contains(out, "cp-1") {
		t.Errorf("status should show cp-1 as head, got:\n%s", out)
	}
	if !strings.Contains(out, "first save") {
		t.Errorf("status should show message, got:\n%s", out)
	}
	if !strings.Contains(out, "clean") {
		t.Errorf("status should show clean (no changes), got:\n%s", out)
	}
	if !strings.Contains(out, "Remote:") {
		t.Errorf("status should show remote section, got:\n%s", out)
	}
	if !strings.Contains(out, "(none)") {
		t.Errorf("status should show no remote, got:\n%s", out)
	}

	// Modify a file and check dirty state.
	writeFile(t, dir, "main.go", "package main\n\nfunc main() { println(\"hi\") }\n")
	out = run(t, dir, "status")
	if !strings.Contains(out, "modified") {
		t.Errorf("status should show modified files, got:\n%s", out)
	}
	if !strings.Contains(out, "bento diff") {
		t.Errorf("status should suggest bento diff, got:\n%s", out)
	}
}

// ---------------------------------------------------------------------------
// TestStatusNoSave: status on initialized but unsaved workspace
// ---------------------------------------------------------------------------

func TestStatusNoSave(t *testing.T) {
	dir := makeWorkspace(t)
	out := run(t, dir, "status")
	if !strings.Contains(out, "none") {
		t.Errorf("status should show no head, got:\n%s", out)
	}
}

// ---------------------------------------------------------------------------
// TestStatusWithRemote: status shows remote sync state
// ---------------------------------------------------------------------------

func TestStatusWithRemote(t *testing.T) {
	registryAddr, cleanup := startRegistry(t)
	defer cleanup()

	dir := makeWorkspace(t)
	run(t, dir, "save", "--skip-secret-scan", "-m", "first")

	repoName := fmt.Sprintf("bento-e2e-status-%d", time.Now().UnixNano()%100000)
	remote := registryAddr + "/" + repoName
	run(t, dir, "push", remote)

	// Status should show "up to date".
	out := run(t, dir, "status")
	if !strings.Contains(out, "up to date") {
		t.Errorf("status should show up to date, got:\n%s", out)
	}
	if !strings.Contains(out, remote) {
		t.Errorf("status should show remote URL, got:\n%s", out)
	}

	// Save locally without pushing — should show "ahead".
	writeFile(t, dir, "new.txt", "new\n")
	run(t, dir, "save", "--skip-secret-scan", "-m", "unpushed")

	out = run(t, dir, "status")
	if !strings.Contains(out, "ahead") {
		t.Errorf("status should show ahead, got:\n%s", out)
	}
	if !strings.Contains(out, "bento push") {
		t.Errorf("status should suggest bento push, got:\n%s", out)
	}
}

// ---------------------------------------------------------------------------
// TestStatusBehindRemote: status shows "behind" when remote has more
// ---------------------------------------------------------------------------

func TestStatusBehindRemote(t *testing.T) {
	registryAddr, cleanup := startRegistry(t)
	defer cleanup()

	repoName := fmt.Sprintf("bento-e2e-behind-%d", time.Now().UnixNano()%100000)
	remote := registryAddr + "/" + repoName

	// Workspace A: save cp-1 and cp-2, push both.
	dirA := makeWorkspace(t)
	run(t, dirA, "save", "--skip-secret-scan", "-m", "first")
	writeFile(t, dirA, "extra.txt", "extra\n")
	run(t, dirA, "save", "--skip-secret-scan", "-m", "second")
	run(t, dirA, "push", remote)

	// Workspace B: open cp-1, configure remote.
	dirB := t.TempDir()
	storeDirB := t.TempDir()
	os.WriteFile(filepath.Join(dirB, "bento.yaml"), []byte("store: "+storeDirB+"\n"), 0644)
	run(t, dirB, "open", remote+":cp-1", dirB)

	// Status should show "behind".
	out := run(t, dirB, "status")
	if !strings.Contains(out, "behind") {
		t.Errorf("status should show behind, got:\n%s", out)
	}
}

// ---------------------------------------------------------------------------
// TestFullRoundTrip: laptop → server → laptop flow
// ---------------------------------------------------------------------------

func TestFullRoundTrip(t *testing.T) {
	registryAddr, cleanup := startRegistry(t)
	defer cleanup()

	repoName := fmt.Sprintf("bento-e2e-roundtrip-%d", time.Now().UnixNano()%100000)
	remote := registryAddr + "/" + repoName

	// === LAPTOP: init, save, push ===
	laptop := makeWorkspace(t)
	run(t, laptop, "save", "--skip-secret-scan", "-m", "leaving for server")
	run(t, laptop, "push", remote)

	// Verify remote persisted on laptop.
	data, _ := os.ReadFile(filepath.Join(laptop, "bento.yaml"))
	if !strings.Contains(string(data), "remote: "+remote) {
		t.Fatalf("laptop bento.yaml should have remote, got:\n%s", data)
	}

	// === SERVER: open from registry (cold start) ===
	server := t.TempDir()
	run(t, server, "open", remote+":latest", server)

	// Verify remote persisted on server.
	data, _ = os.ReadFile(filepath.Join(server, "bento.yaml"))
	if !strings.Contains(string(data), "remote: "+remote) {
		t.Fatalf("server bento.yaml should have remote, got:\n%s", data)
	}

	// Verify files restored.
	if _, err := os.Stat(filepath.Join(server, "main.go")); os.IsNotExist(err) {
		t.Fatal("main.go should be restored on server")
	}

	// Work on server, save, push (no URL needed).
	writeFile(t, server, "server-work.txt", "done on server\n")
	run(t, server, "save", "--skip-secret-scan", "-m", "server work done")
	pushOut := run(t, server, "push")
	if !strings.Contains(pushOut, "Done") {
		t.Fatalf("server push should work without URL, got:\n%s", pushOut)
	}

	// === LAPTOP: pull and open ===
	run(t, laptop, "pull")
	listOut := run(t, laptop, "list")
	if !strings.Contains(listOut, "server work done") {
		t.Errorf("laptop list should show server checkpoint after pull, got:\n%s", listOut)
	}

	run(t, laptop, "open", "latest")

	// Verify server work is on laptop.
	content, err := os.ReadFile(filepath.Join(laptop, "server-work.txt"))
	if err != nil {
		t.Fatalf("server-work.txt should be on laptop after open: %v", err)
	}
	if string(content) != "done on server\n" {
		t.Errorf("server-work.txt content mismatch: %q", content)
	}

	// Status should show up to date.
	statusOut := run(t, laptop, "status")
	if !strings.Contains(statusOut, "up to date") {
		t.Errorf("laptop status should show up to date, got:\n%s", statusOut)
	}
}
