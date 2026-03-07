package signing

import (
	"bufio"
	"crypto/ed25519"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// TrustedKeysFile is the path relative to the grew root where trusted
// public keys are stored, one hex-encoded key per line.
const TrustedKeysFile = "etc/trusted-keys"

// LoadTrustedKeys reads trusted public keys from <grewRoot>/etc/trusted-keys.
// Blank lines and lines starting with '#' are skipped. Returns (nil, nil) if
// the file does not exist.
func LoadTrustedKeys(grewRoot string) ([]ed25519.PublicKey, error) {
	path := filepath.Join(grewRoot, TrustedKeysFile)
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("open trusted keys: %w", err)
	}
	defer f.Close()

	var keys []ed25519.PublicKey
	scanner := bufio.NewScanner(f)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Try SSH public key format first ("ssh-ed25519 AAAA... comment").
		var pub ed25519.PublicKey
		if strings.HasPrefix(line, "ssh-ed25519 ") {
			var sshErr error
			pub, sshErr = ParseSSHPublicKey(line)
			if sshErr != nil {
				return nil, fmt.Errorf("trusted-keys line %d: %w", lineNo, sshErr)
			}
		} else {
			// Fall back to hex-encoded raw key.
			var hexErr error
			pub, hexErr = DecodePublicKey(line)
			if hexErr != nil {
				return nil, fmt.Errorf("trusted-keys line %d: %w", lineNo, hexErr)
			}
		}
		keys = append(keys, pub)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read trusted keys: %w", err)
	}
	return keys, nil
}

// VerifyAny returns true if any of the provided keys successfully verifies
// the signature over the given SHA256 hex string.
func VerifyAny(keys []ed25519.PublicKey, sha256Hex, signatureB64 string) bool {
	for _, key := range keys {
		if Verify(key, sha256Hex, signatureB64) {
			return true
		}
	}
	return false
}
