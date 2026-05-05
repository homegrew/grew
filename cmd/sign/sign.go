package sign

import (
	"crypto/ed25519"
	"fmt"
	"log/slog"

	"github.com/homegrew/grew/pkg/context"
	"github.com/homegrew/grew/internal/signing"
	"github.com/spf13/cobra"
)

var Command = &cobra.Command{
	Use:   "sign <formula> <private-key-or-path>",
	Short: "Sign the SHA256 hashes in a formula with an Ed25519 private key",
	Long: `Sign the SHA256 hashes in a formula with an Ed25519 private key. Prints
YAML-formatted signature fields that can be pasted into the formula file.

The key argument can be:
  - A hex-encoded Ed25519 seed (64 hex characters)
  - A path to an OpenSSH private key file (ssh-keygen -t ed25519)

Generate a key pair with ssh-keygen:
  ssh-keygen -t ed25519 -f grew-signing-key -N ""

Add the public key to etc/trusted-keys on machines that verify signatures.
The trusted-keys file accepts both formats:
  - ssh-ed25519 AAAA... comment    (paste the .pub file line directly)
  - <64 hex chars>                 (raw hex-encoded public key)`,
	Example: `  grew sign jq ~/.ssh/grew-signing-key
  grew sign jq 0123456789abcdef...`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		slog.Debug("starting sign command execution")
		name := args[0]
		keyArg := args[1]

		privKey, err := signing.DecodePrivateKey(keyArg)
		if err != nil {
			return fmt.Errorf("invalid private key: %w", err)
		}

		ctx, err := context.New()
		if err != nil {
			return err
		}

		f, err := ctx.Loader.LoadByName(name)
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

		return nil
	},
}

func init() {
}
