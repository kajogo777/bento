package keys

import (
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
