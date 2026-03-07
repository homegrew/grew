package signing

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/binary"
	"encoding/pem"
	"os"
	"os/exec"
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

func TestParseSSHPrivateKey(t *testing.T) {
	// Generate a key with our code, then encode it as SSH format,
	// and verify round-trip. We'll use ssh-keygen if available.
	kp, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	// Build an OpenSSH private key file manually for the test.
	sshPrivData := buildTestSSHPrivateKey(kp.PrivateKey)
	parsed, err := ParseSSHPrivateKey(sshPrivData)
	if err != nil {
		t.Fatalf("ParseSSHPrivateKey: %v", err)
	}

	// Verify the parsed key can sign and the original public key can verify.
	sha := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	sig := Sign(parsed, sha)
	if !Verify(kp.PublicKey, sha, sig) {
		t.Fatal("signature from parsed SSH key should verify with original public key")
	}
}

func TestParseSSHPublicKey(t *testing.T) {
	kp, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	// Build SSH public key line.
	sshPubLine := buildTestSSHPublicKeyLine(kp.PublicKey)
	parsed, err := ParseSSHPublicKey(sshPubLine)
	if err != nil {
		t.Fatalf("ParseSSHPublicKey: %v", err)
	}

	if !kp.PublicKey.Equal(parsed) {
		t.Fatal("parsed SSH public key doesn't match original")
	}
}

func TestParseSSHPublicKeyErrors(t *testing.T) {
	if _, err := ParseSSHPublicKey("not-ssh-key"); err == nil {
		t.Fatal("expected error for invalid format")
	}
	if _, err := ParseSSHPublicKey("ssh-rsa AAAA... comment"); err == nil {
		t.Fatal("expected error for non-ed25519 key")
	}
}

func TestDecodePrivateKeySSHFile(t *testing.T) {
	kp, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	// Write SSH private key to a temp file.
	tmpFile := filepath.Join(t.TempDir(), "test-key")
	sshPrivData := buildTestSSHPrivateKey(kp.PrivateKey)
	if err := os.WriteFile(tmpFile, sshPrivData, 0600); err != nil {
		t.Fatalf("write: %v", err)
	}

	// DecodePrivateKey should auto-detect the file path.
	parsed, err := DecodePrivateKey(tmpFile)
	if err != nil {
		t.Fatalf("DecodePrivateKey(file): %v", err)
	}

	sha := "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
	sig := Sign(parsed, sha)
	if !Verify(kp.PublicKey, sha, sig) {
		t.Fatal("signature from file-loaded SSH key should verify")
	}
}

func TestLoadTrustedKeysSSHFormat(t *testing.T) {
	kp, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	root := t.TempDir()
	etcDir := filepath.Join(root, "etc")
	os.MkdirAll(etcDir, 0755)

	sshPubLine := buildTestSSHPublicKeyLine(kp.PublicKey)
	content := "# SSH public key\n" + sshPubLine + "\n"
	os.WriteFile(filepath.Join(etcDir, "trusted-keys"), []byte(content), 0644)

	keys, err := LoadTrustedKeys(root)
	if err != nil {
		t.Fatalf("LoadTrustedKeys: %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("expected 1 key, got %d", len(keys))
	}
	if !keys[0].Equal(kp.PublicKey) {
		t.Fatal("loaded SSH public key doesn't match")
	}
}

// buildTestSSHPrivateKey constructs a minimal OpenSSH private key file
// for an Ed25519 key. This is the format ssh-keygen -t ed25519 produces.
func buildTestSSHPrivateKey(priv ed25519.PrivateKey) []byte {
	pub := priv.Public().(ed25519.PublicKey)

	// Build public key blob: string "ssh-ed25519" + string pubkey
	pubBlob := sshString([]byte("ssh-ed25519"))
	pubBlob = append(pubBlob, sshString(pub)...)

	// Build private section: check1 + check2 + keytype + pubkey + privkey + comment + padding
	var privSection []byte
	check := uint32(0x12345678)
	privSection = append(privSection, sshUint32(check)...)
	privSection = append(privSection, sshUint32(check)...)
	privSection = append(privSection, sshString([]byte("ssh-ed25519"))...)
	privSection = append(privSection, sshString(pub)...)
	privSection = append(privSection, sshString(priv)...) // 64 bytes: seed||pub
	privSection = append(privSection, sshString([]byte("test-comment"))...)
	// Add padding to block size (8).
	for i := 1; len(privSection)%8 != 0; i++ {
		privSection = append(privSection, byte(i))
	}

	// Build full binary.
	var buf []byte
	buf = append(buf, "openssh-key-v1\x00"...)
	buf = append(buf, sshString([]byte("none"))...) // cipher
	buf = append(buf, sshString([]byte("none"))...) // kdf
	buf = append(buf, sshString([]byte(""))...)     // kdfoptions
	buf = append(buf, sshUint32(1)...)               // nkeys
	buf = append(buf, sshString(pubBlob)...)
	buf = append(buf, sshString(privSection)...)

	// PEM encode.
	block := &pem.Block{
		Type:  "OPENSSH PRIVATE KEY",
		Bytes: buf,
	}
	return pem.EncodeToMemory(block)
}

// buildTestSSHPublicKeyLine constructs an SSH public key line.
func buildTestSSHPublicKeyLine(pub ed25519.PublicKey) string {
	blob := sshString([]byte("ssh-ed25519"))
	blob = append(blob, sshString(pub)...)
	return "ssh-ed25519 " + base64.StdEncoding.EncodeToString(blob) + " test-comment"
}

func sshString(data []byte) []byte {
	b := make([]byte, 4+len(data))
	binary.BigEndian.PutUint32(b, uint32(len(data)))
	copy(b[4:], data)
	return b
}

func sshUint32(v uint32) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, v)
	return b
}

func TestRealSSHKeygen(t *testing.T) {
	// Skip if ssh-keygen is not available.
	if _, err := exec.LookPath("ssh-keygen"); err != nil {
		t.Skip("ssh-keygen not available")
	}

	dir := t.TempDir()
	keyFile := filepath.Join(dir, "test-key")

	cmd := exec.Command("ssh-keygen", "-t", "ed25519", "-f", keyFile, "-N", "", "-C", "grew-test")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ssh-keygen failed: %s", out)
	}

	// Load private key from file.
	priv, err := DecodePrivateKey(keyFile)
	if err != nil {
		t.Fatalf("DecodePrivateKey(ssh-keygen file): %v", err)
	}

	// Load public key from .pub file.
	pubData, err := os.ReadFile(keyFile + ".pub")
	if err != nil {
		t.Fatalf("read .pub: %v", err)
	}
	pub, err := ParseSSHPublicKey(string(pubData))
	if err != nil {
		t.Fatalf("ParseSSHPublicKey: %v", err)
	}

	// Sign and verify.
	sha := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	sig := Sign(priv, sha)
	if !Verify(pub, sha, sig) {
		t.Fatal("signature from ssh-keygen key should verify with its public key")
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
