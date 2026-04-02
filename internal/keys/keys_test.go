package keys

import (
	"testing"
)

func TestGenerateKeypair(t *testing.T) {
	pub, priv, err := GenerateKeypair()
	if err != nil {
		t.Fatalf("GenerateKeypair failed: %v", err)
	}
	if pub == [32]byte{} {
		t.Error("public key should not be zero")
	}
	if priv == [32]byte{} {
		t.Error("private key should not be zero")
	}
	// Two calls should produce different keys.
	pub2, _, _ := GenerateKeypair()
	if pub == pub2 {
		t.Error("two calls should produce different public keys")
	}
}

func TestFormatParse_PublicKey_RoundTrip(t *testing.T) {
	pub, _, _ := GenerateKeypair()
	formatted := FormatPublicKey(pub)
	if len(formatted) != len(PrefixPublicKey)+43 {
		t.Errorf("formatted public key wrong length: %d (expected %d)", len(formatted), len(PrefixPublicKey)+43)
	}
	parsed, err := ParsePublicKey(formatted)
	if err != nil {
		t.Fatalf("ParsePublicKey failed: %v", err)
	}
	if parsed != pub {
		t.Error("round-trip mismatch for public key")
	}
}

func TestFormatParse_PrivateKey_RoundTrip(t *testing.T) {
	_, priv, _ := GenerateKeypair()
	formatted := FormatPrivateKey(priv)
	parsed, err := ParsePrivateKey(formatted)
	if err != nil {
		t.Fatalf("ParsePrivateKey failed: %v", err)
	}
	if parsed != priv {
		t.Error("round-trip mismatch for private key")
	}
}

func TestFormatParse_DataKey_RoundTrip(t *testing.T) {
	var key [32]byte
	key[0] = 0x42
	key[31] = 0xFF
	formatted := FormatDataKey(key)
	if formatted[:len(PrefixDataKey)] != PrefixDataKey {
		t.Errorf("formatted data key should start with %q, got %q", PrefixDataKey, formatted[:len(PrefixDataKey)])
	}
	parsed, err := ParseDataKey(formatted)
	if err != nil {
		t.Fatalf("ParseDataKey failed: %v", err)
	}
	if parsed != key {
		t.Error("round-trip mismatch for data key")
	}
}

func TestParsePublicKey_InvalidPrefix(t *testing.T) {
	_, err := ParsePublicKey("bento-sk-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")
	if err == nil {
		t.Error("expected error for wrong prefix")
	}
}

func TestParsePublicKey_WrongLength(t *testing.T) {
	_, err := ParsePublicKey("bento-pk-AAAA")
	if err == nil {
		t.Error("expected error for wrong key length")
	}
}

func TestParseDataKey_InvalidPrefix(t *testing.T) {
	_, err := ParseDataKey("bento-pk-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")
	if err == nil {
		t.Error("expected error for wrong prefix on data key")
	}
}

func TestSaveLoadKeypair(t *testing.T) {
	tmp := t.TempDir()
	pub, priv, _ := GenerateKeypair()

	if err := SaveKeypairTo(tmp, "test", pub, priv); err != nil {
		t.Fatalf("SaveKeypairTo failed: %v", err)
	}

	gotPub, gotPriv, err := LoadKeypairFrom(tmp, "test")
	if err != nil {
		t.Fatalf("LoadKeypairFrom failed: %v", err)
	}
	if gotPub != pub {
		t.Error("loaded public key mismatch")
	}
	if gotPriv != priv {
		t.Error("loaded private key mismatch")
	}
}

func TestLoadDefaultKeypair_Default(t *testing.T) {
	tmp := t.TempDir()
	pub, priv, _ := GenerateKeypair()
	_ = SaveKeypairTo(tmp, "default", pub, priv)

	gotPub, gotPriv, err := LoadDefaultKeypairFrom(tmp)
	if err != nil {
		t.Fatalf("LoadDefaultKeypairFrom failed: %v", err)
	}
	if gotPub != pub || gotPriv != priv {
		t.Error("loaded default keypair mismatch")
	}
}

func TestLoadDefaultKeypair_FallbackAlphabetical(t *testing.T) {
	tmp := t.TempDir()
	pubA, privA, _ := GenerateKeypair()
	pubB, _, _ := GenerateKeypair()
	_ = SaveKeypairTo(tmp, "alpha", pubA, privA)
	_ = SaveKeypairTo(tmp, "beta", pubB, pubB) // reuse pubB as "priv" — doesn't matter for test

	gotPub, _, err := LoadDefaultKeypairFrom(tmp)
	if err != nil {
		t.Fatalf("LoadDefaultKeypairFrom failed: %v", err)
	}
	if gotPub != pubA {
		t.Error("should fall back to alphabetically first keypair")
	}
}

func TestLoadDefaultKeypair_NoKeypair(t *testing.T) {
	tmp := t.TempDir()
	_, _, err := LoadDefaultKeypairFrom(tmp)
	if err != ErrNoKeypair {
		t.Errorf("expected ErrNoKeypair, got %v", err)
	}
}

func TestListKeypairs(t *testing.T) {
	tmp := t.TempDir()
	pub1, priv1, _ := GenerateKeypair()
	pub2, priv2, _ := GenerateKeypair()
	_ = SaveKeypairTo(tmp, "default", pub1, priv1)
	_ = SaveKeypairTo(tmp, "work", pub2, priv2)

	kps, err := ListKeypairsFrom(tmp)
	if err != nil {
		t.Fatalf("ListKeypairsFrom failed: %v", err)
	}
	if len(kps) != 2 {
		t.Fatalf("expected 2 keypairs, got %d", len(kps))
	}
	// default should be first.
	if kps[0].Name != "default" {
		t.Errorf("expected first keypair to be 'default', got %q", kps[0].Name)
	}
	if kps[1].Name != "work" {
		t.Errorf("expected second keypair to be 'work', got %q", kps[1].Name)
	}
}

func TestListKeypairs_Empty(t *testing.T) {
	tmp := t.TempDir()
	kps, err := ListKeypairsFrom(tmp)
	if err != nil {
		t.Fatalf("ListKeypairsFrom failed: %v", err)
	}
	if len(kps) != 0 {
		t.Errorf("expected 0 keypairs, got %d", len(kps))
	}
}
