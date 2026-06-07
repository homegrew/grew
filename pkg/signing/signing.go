package signing

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"log/slog"
	"os"
	"strings"
)

// KeyPair holds an Ed25519 key pair for signing and verification.
type KeyPair struct {
	PublicKey  ed25519.PublicKey  // 32-byte Ed25519 public key
	PrivateKey ed25519.PrivateKey // 64-byte Ed25519 private key (seed || pubkey)
}

// GenerateKey generates a new Ed25519 key pair.
func GenerateKey() (*KeyPair, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		slog.Debug("failed to generate Ed25519 key pair", "error", err)
		return nil, fmt.Errorf("generate ed25519 key: %w", err)
	}
	return &KeyPair{PublicKey: pub, PrivateKey: priv}, nil
}

// Sign signs the raw bytes of the SHA256 hex string and returns a
// base64-encoded Ed25519 signature.
func Sign(privateKey ed25519.PrivateKey, sha256Hex string) string {
	sig := ed25519.Sign(privateKey, []byte(sha256Hex))
	slog.Debug("generated signature", "sha256Hex", sha256Hex, "signature", base64.StdEncoding.EncodeToString(sig))
	return base64.StdEncoding.EncodeToString(sig)
}

// Verify checks that signatureB64 is a valid Ed25519 signature of the
// raw bytes of sha256Hex under publicKey.
func Verify(publicKey ed25519.PublicKey, sha256Hex string, signatureB64 string) bool {
	sig, err := base64.StdEncoding.DecodeString(signatureB64)
	if err != nil {
		slog.Debug("failed to decode signature", "error", err)
		return false
	}
	return ed25519.Verify(publicKey, []byte(sha256Hex), sig)
}

// EncodePublicKey returns the hex-encoded form of an Ed25519 public key.
func EncodePublicKey(pub ed25519.PublicKey) string {
	return hex.EncodeToString(pub)
}

// DecodePublicKey decodes a hex-encoded Ed25519 public key.
func DecodePublicKey(s string) (ed25519.PublicKey, error) {
	b, err := hex.DecodeString(s)
	if err != nil {
		slog.Debug("failed to decode public key hex", "error", err)
		return nil, fmt.Errorf("decode public key hex: %w", err)
	}
	if len(b) != ed25519.PublicKeySize {
		slog.Debug("invalid public key length", "got", len(b), "want", ed25519.PublicKeySize)
		return nil, fmt.Errorf("invalid public key length: got %d, want %d", len(b), ed25519.PublicKeySize)
	}
	return ed25519.PublicKey(b), nil
}

// EncodePrivateKey returns the hex-encoded seed (first 32 bytes) of an
// Ed25519 private key.
func EncodePrivateKey(priv ed25519.PrivateKey) string {
	return hex.EncodeToString(priv.Seed())
}

// DecodePrivateKey decodes an Ed25519 private key from one of:
//   - A hex-encoded 32-byte seed (64 hex characters)
//   - A path to an OpenSSH private key file (ssh-keygen -t ed25519)
//
// It auto-detects the format.
func DecodePrivateKey(s string) (ed25519.PrivateKey, error) {
	// If it looks like a hex seed, decode directly.
	if len(s) == 64 && isHex(s) {
		return decodeHexSeed(s)
	}

	// Try as a file path.
	data, err := os.ReadFile(s)
	if err != nil {
		slog.Debug("failed to read private key file", "path", s, "error", err)
		// Not a file — try hex decode anyway for a better error message.
		return decodeHexSeed(s)
	}

	return ParseSSHPrivateKey(data)
}

func isHex(s string) bool {
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

func decodeHexSeed(s string) (ed25519.PrivateKey, error) {
	seed, err := hex.DecodeString(s)
	if err != nil {
		slog.Debug("failed to decode private key hex", "error", err)
		return nil, fmt.Errorf("decode private key hex: %w", err)
	}
	if len(seed) != ed25519.SeedSize {
		slog.Debug("invalid seed length", "got", len(seed), "want", ed25519.SeedSize)
		return nil, fmt.Errorf("invalid seed length: got %d, want %d", len(seed), ed25519.SeedSize)
	}
	return ed25519.NewKeyFromSeed(seed), nil
}

// ParseSSHPrivateKey extracts an Ed25519 private key from an OpenSSH
// private key file (the format produced by ssh-keygen -t ed25519).
// Only unencrypted keys are supported.
func ParseSSHPrivateKey(data []byte) (ed25519.PrivateKey, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		slog.Debug("no PEM block found in SSH private key")
		return nil, fmt.Errorf("no PEM block found in SSH private key")
	}
	if block.Type != "OPENSSH PRIVATE KEY" {
		slog.Debug("unexpected PEM type", "got", block.Type, "want", "OPENSSH PRIVATE KEY")
		return nil, fmt.Errorf("unexpected PEM type: %s (want OPENSSH PRIVATE KEY)", block.Type)
	}

	// OpenSSH private key binary format:
	//   "openssh-key-v1\0"  (15 bytes magic)
	//   string ciphername
	//   string kdfname
	//   string kdfoptions
	//   uint32 number-of-keys
	//   string public-key-blob
	//   string private-key-blob (padded)
	raw := block.Bytes

	magic := "openssh-key-v1\x00"
	if len(raw) < len(magic) || string(raw[:len(magic)]) != magic {
		slog.Debug("invalid OpenSSH key magic")
		return nil, fmt.Errorf("invalid OpenSSH key magic")
	}
	rest := raw[len(magic):]

	// Read ciphername.
	cipher, rest, err := readSSHString(rest)
	if err != nil {
		slog.Debug("failed to read ciphername from SSH private key", "error", err)
		return nil, fmt.Errorf("read ciphername: %w", err)
	}
	if string(cipher) != "none" {
		slog.Debug("encrypted SSH keys are not supported", "cipher", cipher)
		return nil, fmt.Errorf("encrypted SSH keys are not supported (cipher: %s)", cipher)
	}

	// Read kdfname.
	_, rest, err = readSSHString(rest)
	if err != nil {
		slog.Debug("failed to read kdfname from SSH private key", "error", err)
		return nil, fmt.Errorf("read kdfname: %w", err)
	}

	// Read kdfoptions.
	_, rest, err = readSSHString(rest)
	if err != nil {
		slog.Debug("failed to read kdfoptions from SSH private key", "error", err)
		return nil, fmt.Errorf("read kdfoptions: %w", err)
	}

	// Read number of keys.
	if len(rest) < 4 {
		slog.Debug("SSH private key data too short for number of keys")
		return nil, fmt.Errorf("truncated key data")
	}
	nKeys := binary.BigEndian.Uint32(rest[:4])
	rest = rest[4:]
	if nKeys != 1 {
		slog.Debug("unexpected number of keys in SSH private key", "got", nKeys, "want", 1)
		return nil, fmt.Errorf("expected 1 key, got %d", nKeys)
	}

	// Skip public key blob.
	_, rest, err = readSSHString(rest)
	if err != nil {
		slog.Debug("failed to read public key blob from SSH private key", "error", err)
		return nil, fmt.Errorf("read public key blob: %w", err)
	}

	// Read private key blob.
	privBlob, _, err := readSSHString(rest)
	if err != nil {
		slog.Debug("failed to read private key blob from SSH private key", "error", err)
		return nil, fmt.Errorf("read private key blob: %w", err)
	}

	// Private key blob format:
	//   uint32 checkint1
	//   uint32 checkint2
	//   string keytype ("ssh-ed25519")
	//   string pubkey (32 bytes)
	//   string privkey (64 bytes: 32-byte seed || 32-byte pubkey)
	//   string comment
	//   padding
	if len(privBlob) < 8 {
		slog.Debug("SSH private key blob too short")
		return nil, fmt.Errorf("private blob too short")
	}
	check1 := binary.BigEndian.Uint32(privBlob[:4])
	check2 := binary.BigEndian.Uint32(privBlob[4:8])
	if check1 != check2 {
		slog.Debug("SSH private key checkint mismatch (key may be encrypted)", "check1", check1, "check2", check2)
		return nil, fmt.Errorf("check bytes mismatch (key may be encrypted)")
	}
	inner := privBlob[8:]

	// Read keytype.
	keytype, inner, err := readSSHString(inner)
	if err != nil {
		slog.Debug("failed to read key type from SSH private key blob", "error", err)
		return nil, fmt.Errorf("read keytype: %w", err)
	}
	if string(keytype) != "ssh-ed25519" {
		slog.Debug("unexpected key type in SSH private key blob", "got", keytype)
		return nil, fmt.Errorf("not an Ed25519 key (type: %s)", keytype)
	}

	// Skip pubkey string.
	_, inner, err = readSSHString(inner)
	if err != nil {
		slog.Debug("failed to read public key from SSH private key blob", "error", err)
		return nil, fmt.Errorf("read inner pubkey: %w", err)
	}

	// Read the 64-byte private key (seed || pubkey).
	privBytes, _, err := readSSHString(inner)
	if err != nil {
		slog.Debug("failed to read private key from SSH private key blob", "error", err)
		return nil, fmt.Errorf("read inner privkey: %w", err)
	}
	if len(privBytes) != ed25519.PrivateKeySize {
		slog.Debug("unexpected private key size", "got", len(privBytes), "want", ed25519.PrivateKeySize)
		return nil, fmt.Errorf("unexpected private key size: %d", len(privBytes))
	}

	return ed25519.PrivateKey(privBytes), nil
}

// ParseSSHPublicKey extracts an Ed25519 public key from an SSH public key
// line (the format produced by ssh-keygen: "ssh-ed25519 AAAA... comment").
func ParseSSHPublicKey(line string) (ed25519.PublicKey, error) {
	parts := strings.Fields(line)
	if len(parts) < 2 {
		slog.Debug("invalid SSH public key format", "line", line)
		return nil, fmt.Errorf("invalid SSH public key format")
	}
	if parts[0] != "ssh-ed25519" {
		slog.Debug("not an Ed25519 SSH key", "type", parts[0])
		return nil, fmt.Errorf("not an Ed25519 SSH key (type: %s)", parts[0])
	}

	blob, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		slog.Debug("failed to decode SSH public key base64", "error", err)
		return nil, fmt.Errorf("decode SSH public key base64: %w", err)
	}

	// blob format: [4-byte len]["ssh-ed25519"][4-byte len][32-byte pubkey]
	keytype, rest, err := readSSHString(blob)
	if err != nil {
		slog.Debug("failed to read key type from SSH public key blob", "error", err)
		return nil, fmt.Errorf("read key type from blob: %w", err)
	}
	if string(keytype) != "ssh-ed25519" {
		slog.Debug("unexpected key type in SSH public key blob", "got", keytype)
		return nil, fmt.Errorf("key type mismatch in blob: %s", keytype)
	}

	pubBytes, _, err := readSSHString(rest)
	if err != nil {
		slog.Debug("failed to read public key from SSH public key blob", "error", err)
		return nil, fmt.Errorf("read public key from blob: %w", err)
	}
	if len(pubBytes) != ed25519.PublicKeySize {
		slog.Debug("unexpected public key size in SSH public key blob", "got", len(pubBytes), "want", ed25519.PublicKeySize)
		return nil, fmt.Errorf("unexpected public key size: %d", len(pubBytes))
	}

	return ed25519.PublicKey(pubBytes), nil
}

// readSSHString reads a length-prefixed string from an SSH wire format buffer.
func readSSHString(data []byte) ([]byte, []byte, error) {
	if len(data) < 4 {
		slog.Debug("buffer too short to read SSH string length")
		return nil, nil, fmt.Errorf("buffer too short for string length")
	}
	n := binary.BigEndian.Uint32(data[:4])
	data = data[4:]
	if uint32(len(data)) < n {
		return nil, nil, fmt.Errorf("buffer too short for string data: need %d, have %d", n, len(data))
	}
	return data[:n], data[n:], nil
}
