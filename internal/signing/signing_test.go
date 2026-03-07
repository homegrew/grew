package signing

import (
	"crypto/ed25519"
	"os"
	"path/filepath"
	"testing"
)

func TestGenerateAndSign(t *testing.T) {
	kp, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	sha := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	sig := Sign(kp.PrivateKey, sha)

	if !Verify(kp.PublicKey, sha, sig) {
		t.Fatal("Verify should succeed with correct key and hash")
	}
}

func TestVerifyWrongKey(t *testing.T) {
	kp1, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	kp2, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	sha := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	sig := Sign(kp1.PrivateKey, sha)

	if Verify(kp2.PublicKey, sha, sig) {
		t.Fatal("Verify should fail with wrong key")
	}
}

func TestVerifyTamperedHash(t *testing.T) {
	kp, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	sha1 := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	sha2 := "a3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	sig := Sign(kp.PrivateKey, sha1)

	if Verify(kp.PublicKey, sha2, sig) {
		t.Fatal("Verify should fail with tampered hash")
	}
}

func TestKeyEncodeDecode(t *testing.T) {
	kp, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	// Public key round-trip.
	pubHex := EncodePublicKey(kp.PublicKey)
	pubDecoded, err := DecodePublicKey(pubHex)
	if err != nil {
		t.Fatalf("DecodePublicKey: %v", err)
	}
	if !kp.PublicKey.Equal(pubDecoded) {
		t.Fatal("public key round-trip mismatch")
	}

	// Private key round-trip.
	privHex := EncodePrivateKey(kp.PrivateKey)
	privDecoded, err := DecodePrivateKey(privHex)
	if err != nil {
		t.Fatalf("DecodePrivateKey: %v", err)
	}
	if !kp.PrivateKey.Equal(privDecoded) {
		t.Fatal("private key round-trip mismatch")
	}

	// Verify that the decoded private key produces valid signatures.
	sha := "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
	sig := Sign(privDecoded, sha)
	if !Verify(pubDecoded, sha, sig) {
		t.Fatal("decoded keys should produce valid signatures")
	}
}

func TestDecodePublicKeyErrors(t *testing.T) {
	// Invalid hex.
	if _, err := DecodePublicKey("not-hex"); err == nil {
		t.Fatal("expected error for invalid hex")
	}
	// Wrong length.
	if _, err := DecodePublicKey("abcd"); err == nil {
		t.Fatal("expected error for wrong length")
	}
}

func TestDecodePrivateKeyErrors(t *testing.T) {
	// Invalid hex.
	if _, err := DecodePrivateKey("not-hex"); err == nil {
		t.Fatal("expected error for invalid hex")
	}
	// Wrong length.
	if _, err := DecodePrivateKey("abcd"); err == nil {
		t.Fatal("expected error for wrong length")
	}
}

func TestVerifyBadBase64(t *testing.T) {
	kp, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	sha := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	if Verify(kp.PublicKey, sha, "!!!not-base64!!!") {
		t.Fatal("Verify should fail with invalid base64")
	}
}

func TestLoadTrustedKeys(t *testing.T) {
	kp1, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	kp2, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	root := t.TempDir()
	etcDir := filepath.Join(root, "etc")
	if err := os.MkdirAll(etcDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	content := "# This is a comment\n" +
		"\n" +
		EncodePublicKey(kp1.PublicKey) + "\n" +
		"# Another comment\n" +
		EncodePublicKey(kp2.PublicKey) + "\n" +
		"\n"

	if err := os.WriteFile(filepath.Join(etcDir, "trusted-keys"), []byte(content), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	keys, err := LoadTrustedKeys(root)
	if err != nil {
		t.Fatalf("LoadTrustedKeys: %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(keys))
	}
	if !keys[0].Equal(kp1.PublicKey) {
		t.Fatal("first key mismatch")
	}
	if !keys[1].Equal(kp2.PublicKey) {
		t.Fatal("second key mismatch")
	}
}

func TestLoadTrustedKeysMissing(t *testing.T) {
	root := t.TempDir()
	keys, err := LoadTrustedKeys(root)
	if err != nil {
		t.Fatalf("expected nil error for missing file, got: %v", err)
	}
	if keys != nil {
		t.Fatalf("expected nil keys for missing file, got %d", len(keys))
	}
}

func TestVerifyAny(t *testing.T) {
	kp1, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	kp2, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	kp3, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	sha := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	sig := Sign(kp2.PrivateKey, sha)

	// Should succeed: kp2 is in the list.
	trustedKeys := []ed25519.PublicKey{kp1.PublicKey, kp2.PublicKey}
	if !VerifyAny(trustedKeys, sha, sig) {
		t.Fatal("VerifyAny should succeed when correct key is present")
	}

	// Should fail: kp2 is NOT in the list.
	wrongKeys := []ed25519.PublicKey{kp1.PublicKey, kp3.PublicKey}
	if VerifyAny(wrongKeys, sha, sig) {
		t.Fatal("VerifyAny should fail when correct key is absent")
	}

	// Should fail: empty key list.
	if VerifyAny(nil, sha, sig) {
		t.Fatal("VerifyAny should fail with empty key list")
	}
}
