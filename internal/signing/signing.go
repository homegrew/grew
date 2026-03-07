package signing

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

// KeyPair holds an Ed25519 key pair for signing and verification.
type KeyPair struct {
	PublicKey  ed25519.PublicKey
	PrivateKey ed25519.PrivateKey
}

// GenerateKey generates a new Ed25519 key pair.
func GenerateKey() (*KeyPair, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate ed25519 key: %w", err)
	}
	return &KeyPair{PublicKey: pub, PrivateKey: priv}, nil
}

// Sign signs the raw bytes of the SHA256 hex string and returns a
// base64-encoded Ed25519 signature.
func Sign(privateKey ed25519.PrivateKey, sha256Hex string) string {
	sig := ed25519.Sign(privateKey, []byte(sha256Hex))
	return base64.StdEncoding.EncodeToString(sig)
}

// Verify checks that signatureB64 is a valid Ed25519 signature of the
// raw bytes of sha256Hex under publicKey.
func Verify(publicKey ed25519.PublicKey, sha256Hex string, signatureB64 string) bool {
	sig, err := base64.StdEncoding.DecodeString(signatureB64)
	if err != nil {
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
		return nil, fmt.Errorf("decode public key hex: %w", err)
	}
	if len(b) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("invalid public key length: got %d, want %d", len(b), ed25519.PublicKeySize)
	}
	return ed25519.PublicKey(b), nil
}

// EncodePrivateKey returns the hex-encoded seed (first 32 bytes) of an
// Ed25519 private key.
func EncodePrivateKey(priv ed25519.PrivateKey) string {
	return hex.EncodeToString(priv.Seed())
}

// DecodePrivateKey decodes a hex-encoded Ed25519 seed and expands it to
// the full 64-byte private key.
func DecodePrivateKey(s string) (ed25519.PrivateKey, error) {
	seed, err := hex.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("decode private key hex: %w", err)
	}
	if len(seed) != ed25519.SeedSize {
		return nil, fmt.Errorf("invalid seed length: got %d, want %d", len(seed), ed25519.SeedSize)
	}
	return ed25519.NewKeyFromSeed(seed), nil
}
