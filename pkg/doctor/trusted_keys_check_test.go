package doctor

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/homegrew/grew/pkg/config"
)

func TestCheckTrustedKeys(t *testing.T) {
	t.Parallel()

	t.Run("valid key present — no warning", func(t *testing.T) {
		t.Parallel()
		tmp := t.TempDir()
		etcDir := filepath.Join(tmp, "etc")
		os.MkdirAll(etcDir, 0755)
		// A valid 32-byte Ed25519 public key in hex (64 hex chars).
		validKey := "4355a46b19d348dc2f57c046f8ef63d4538ebb936000f3c9ee954a27460dd865"
		os.WriteFile(filepath.Join(etcDir, "trusted-keys"), []byte(validKey+"\n"), 0644)

		ctx := &Context{Paths: config.Paths{Root: tmp}}
		assertDoctorWarnings(t, ctx, CheckTrustedKeys, "")
	})

	t.Run("file missing — warns", func(t *testing.T) {
		t.Parallel()
		tmp := t.TempDir()
		// No etc/trusted-keys file at all.
		ctx := &Context{Paths: config.Paths{Root: tmp}}
		assertDoctorWarnings(t, ctx, CheckTrustedKeys, "no trusted Ed25519 keys")
	})

	t.Run("file exists but empty — warns", func(t *testing.T) {
		t.Parallel()
		tmp := t.TempDir()
		etcDir := filepath.Join(tmp, "etc")
		os.MkdirAll(etcDir, 0755)
		os.WriteFile(filepath.Join(etcDir, "trusted-keys"), []byte("# just a comment\n\n"), 0644)

		ctx := &Context{Paths: config.Paths{Root: tmp}}
		assertDoctorWarnings(t, ctx, CheckTrustedKeys, "no trusted Ed25519 keys")
	})

	t.Run("malformed key — warns", func(t *testing.T) {
		t.Parallel()
		tmp := t.TempDir()
		etcDir := filepath.Join(tmp, "etc")
		os.MkdirAll(etcDir, 0755)
		os.WriteFile(filepath.Join(etcDir, "trusted-keys"), []byte("not-a-valid-key\n"), 0644)

		ctx := &Context{Paths: config.Paths{Root: tmp}}
		assertDoctorWarnings(t, ctx, CheckTrustedKeys, "malformed")
	})
}
