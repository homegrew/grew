package cmd

import (
	"crypto/ed25519"
	"fmt"

	"github.com/homegrew/grew/internal/config"
	"github.com/homegrew/grew/internal/signing"
	"github.com/homegrew/grew/internal/tap"
)

func runSign(args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("usage: grew sign <formula> <private-key-hex>")
	}

	name := args[0]
	privHex := args[1]

	privKey, err := signing.DecodePrivateKey(privHex)
	if err != nil {
		return fmt.Errorf("invalid private key: %w", err)
	}

	paths := config.Default()
	if err := paths.Init(); err != nil {
		return err
	}

	tapMgr := &tap.Manager{TapsDir: paths.Taps}
	if err := tapMgr.InitCore(); err != nil {
		return fmt.Errorf("init core tap: %w", err)
	}

	loader := newLoader(paths.Taps)
	f, err := loader.LoadByName(name)
	if err != nil {
		return fmt.Errorf("formula not found: %s", name)
	}

	pubKey := privKey.Public().(ed25519.PublicKey)
	fmt.Printf("# Signatures for %s %s\n", f.Name, f.Version)
	fmt.Printf("# Public key: %s\n", signing.EncodePublicKey(pubKey))

	// Sign bottle SHA256s (new format).
	if len(f.Bottle) > 0 {
		fmt.Printf("bottle:\n")
		for platform, b := range f.Bottle {
			if b.SHA256 == "" {
				continue
			}
			sig := signing.Sign(privKey, b.SHA256)
			fmt.Printf("  %s:\n", platform)
			fmt.Printf("    url: %s\n", b.URL)
			fmt.Printf("    sha256: %s\n", b.SHA256)
			fmt.Printf("    signature: %s\n", sig)
		}
	}

	// Sign legacy SHA256s.
	if len(f.SHA256) > 0 {
		fmt.Printf("signature:\n")
		for platform, sha := range f.SHA256 {
			sig := signing.Sign(privKey, sha)
			fmt.Printf("  %s: %s\n", platform, sig)
		}
	}

	// Sign source SHA256.
	if f.Source.SHA256 != "" {
		sig := signing.Sign(privKey, f.Source.SHA256)
		fmt.Printf("source:\n")
		fmt.Printf("  url: %s\n", f.Source.URL)
		fmt.Printf("  sha256: %s\n", f.Source.SHA256)
		fmt.Printf("  signature: %s\n", sig)
	}

	// Sign legacy source SHA256.
	if f.SourceSHA256 != "" {
		sig := signing.Sign(privKey, f.SourceSHA256)
		fmt.Printf("# source_sha256 signature: %s\n", sig)
	}

	return nil
}
