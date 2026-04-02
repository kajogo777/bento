package keys

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveRecipients_LiteralKey(t *testing.T) {
	pub, _, _ := GenerateKeypair()
	senderPub, _, _ := GenerateKeypair()
	keysDir := t.TempDir()

	recipients, err := ResolveRecipients(
		[]string{FormatPublicKey(pub)},
		nil,
		senderPub,
		keysDir,
	)
	if err != nil {
		t.Fatalf("ResolveRecipients failed: %v", err)
	}
	// Should include both the literal key and the sender.
	if len(recipients) != 2 {
		t.Fatalf("expected 2 recipients, got %d", len(recipients))
	}
	if recipients[0] != pub {
		t.Error("first recipient should be the literal key")
	}
	if recipients[1] != senderPub {
		t.Error("second recipient should be the sender")
	}
}

func TestResolveRecipients_ConfigName(t *testing.T) {
	alicePub, _, _ := GenerateKeypair()
	senderPub, _, _ := GenerateKeypair()
	keysDir := t.TempDir()

	configRecipients := []ConfigRecipient{
		{Name: "alice", Key: FormatPublicKey(alicePub)},
	}

	recipients, err := ResolveRecipients(
		[]string{"alice"},
		configRecipients,
		senderPub,
		keysDir,
	)
	if err != nil {
		t.Fatalf("ResolveRecipients failed: %v", err)
	}
	if len(recipients) != 2 {
		t.Fatalf("expected 2 recipients, got %d", len(recipients))
	}
	if recipients[0] != alicePub {
		t.Error("first recipient should be alice's key")
	}
}

func TestResolveRecipients_FileRecipient(t *testing.T) {
	bobPub, _, _ := GenerateKeypair()
	senderPub, _, _ := GenerateKeypair()
	keysDir := t.TempDir()

	// Write bob's .pub file.
	if err := AddRecipientTo(keysDir, "bob", FormatPublicKey(bobPub)); err != nil {
		t.Fatalf("AddRecipientTo failed: %v", err)
	}

	recipients, err := ResolveRecipients(
		[]string{"bob"},
		nil,
		senderPub,
		keysDir,
	)
	if err != nil {
		t.Fatalf("ResolveRecipients failed: %v", err)
	}
	if len(recipients) != 2 {
		t.Fatalf("expected 2 recipients, got %d", len(recipients))
	}
	if recipients[0] != bobPub {
		t.Error("first recipient should be bob's key")
	}
}

func TestResolveRecipients_UnknownName(t *testing.T) {
	senderPub, _, _ := GenerateKeypair()
	keysDir := t.TempDir()

	_, err := ResolveRecipients(
		[]string{"unknown"},
		nil,
		senderPub,
		keysDir,
	)
	if err == nil {
		t.Fatal("expected error for unknown recipient")
	}
}

func TestResolveRecipients_SenderDedup(t *testing.T) {
	senderPub, _, _ := GenerateKeypair()
	keysDir := t.TempDir()

	// If sender's key is passed as a specifier, should not be duplicated.
	recipients, err := ResolveRecipients(
		[]string{FormatPublicKey(senderPub)},
		nil,
		senderPub,
		keysDir,
	)
	if err != nil {
		t.Fatalf("ResolveRecipients failed: %v", err)
	}
	if len(recipients) != 1 {
		t.Fatalf("expected 1 recipient (deduped), got %d", len(recipients))
	}
}

func TestResolveRecipients_EmptySpecifiers(t *testing.T) {
	senderPub, _, _ := GenerateKeypair()
	keysDir := t.TempDir()

	// No specifiers: should still include sender.
	recipients, err := ResolveRecipients(nil, nil, senderPub, keysDir)
	if err != nil {
		t.Fatalf("ResolveRecipients failed: %v", err)
	}
	if len(recipients) != 1 {
		t.Fatalf("expected 1 recipient (sender), got %d", len(recipients))
	}
	if recipients[0] != senderPub {
		t.Error("only recipient should be the sender")
	}
}

func TestAddRemoveRecipient(t *testing.T) {
	keysDir := t.TempDir()
	pub, _, _ := GenerateKeypair()

	if err := AddRecipientTo(keysDir, "test", FormatPublicKey(pub)); err != nil {
		t.Fatalf("AddRecipientTo failed: %v", err)
	}

	// Verify file exists.
	path := filepath.Join(keysDir, "recipients", "test.pub")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("recipient file not created: %v", err)
	}

	// Load it back.
	loaded, err := LoadRecipientFile(keysDir, "test")
	if err != nil {
		t.Fatalf("LoadRecipientFile failed: %v", err)
	}
	if loaded != pub {
		t.Error("loaded key mismatch")
	}

	// Remove it.
	if err := RemoveRecipientFrom(keysDir, "test"); err != nil {
		t.Fatalf("RemoveRecipientFrom failed: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("recipient file should be deleted")
	}
}

func TestRemoveRecipient_NotFound(t *testing.T) {
	keysDir := t.TempDir()
	err := RemoveRecipientFrom(keysDir, "nonexistent")
	if err == nil {
		t.Fatal("expected error for removing nonexistent recipient")
	}
}

func TestAddRecipient_InvalidKey(t *testing.T) {
	keysDir := t.TempDir()
	err := AddRecipientTo(keysDir, "test", "not-a-key")
	if err == nil {
		t.Fatal("expected error for invalid key")
	}
}

func TestListRecipients(t *testing.T) {
	keysDir := t.TempDir()
	pub, _, _ := GenerateKeypair()

	_ = AddRecipientTo(keysDir, "alice", FormatPublicKey(pub))

	configRecipients := []ConfigRecipient{
		{Name: "bob", Key: FormatPublicKey(pub)},
	}

	result := ListRecipients(configRecipients, keysDir)
	if len(result) != 2 {
		t.Fatalf("expected 2 recipients, got %d", len(result))
	}
	// bob from config should be first (config comes first).
	if result[0].Name != "bob" || result[0].Source != "bento.yaml" {
		t.Errorf("expected first to be bob from bento.yaml, got %v", result[0])
	}
}

func TestParseRecipientFileContent_Comments(t *testing.T) {
	content := "# This is a comment\n\n# Another comment\nbento-pk-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA\n"
	pk, err := parseRecipientFileContent([]byte(content))
	if err != nil {
		t.Fatalf("parseRecipientFileContent failed: %v", err)
	}
	// Verify the key is all zeros (base64url "AAA..." decodes to 0x00...).
	for _, b := range pk {
		if b != 0 {
			t.Fatalf("expected all zeros key, got non-zero byte")
		}
	}
}

func TestParseRecipientFileContent_Empty(t *testing.T) {
	_, err := parseRecipientFileContent([]byte("# only comments\n"))
	if err == nil {
		t.Fatal("expected error for empty/comment-only file")
	}
}
