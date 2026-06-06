package installer

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// FuzzVersionHealthChecker feeds arbitrary byte sequences as the stdout output
// of a fake binary and verifies that the checker never panics and respects
// its core decision rules regardless of what the binary prints.
//
// Run with:
//
//	go test ./pkg/installer/... -fuzz=FuzzVersionHealthChecker -fuzztime=60s
func FuzzVersionHealthChecker(f *testing.F) {
	// Seed: exact version match.
	f.Add("grew 1.2.3", "1.2.3", 0)
	// Seed: dev build bypasses mismatch.
	f.Add("grew dev", "9.9.9", 0)
	// Seed: empty output — must be rejected.
	f.Add("", "", 0)
	// Seed: only whitespace — treated as empty.
	f.Add("   \t\n  ", "1.0.0", 0)
	// Seed: version with v-prefix.
	f.Add("grew v2.0.0", "2.0.0", 0)
	// Seed: very long line.
	f.Add(strings.Repeat("x", 65536), "1.0.0", 0)
	// Seed: version mismatch.
	f.Add("grew 0.0.1", "1.0.0", 0)
	// Seed: binary exits non-zero (exitCode=1).
	f.Add("", "", 1)
	// Seed: multi-line output containing the version somewhere.
	f.Add("debug info\ngrew 3.0.0\nextra info", "3.0.0", 0)
	// Seed: null bytes.
	f.Add("grew\x001.0.0", "1.0.0", 0)
	// Seed: non-UTF-8 bytes.
	f.Add("grew \xff\xfe1.0.0", "1.0.0", 0)

	f.Fuzz(func(t *testing.T, output string, expected string, exitCode int) {
		// Clamp exit code to 0 or 1 so we get meaningful results.
		if exitCode < 0 {
			exitCode = 0
		}
		exitCode = exitCode % 2 // 0 = success, 1 = failure

		// Build a real temporary script that emits output and exits with exitCode.
		dir := t.TempDir()
		bin := filepath.Join(dir, "grew")

		// Sanitize output for embedding in a shell echo statement:
		// write the bytes directly to a tmpfile and cat it instead.
		outFile := filepath.Join(dir, "out.txt")
		if err := os.WriteFile(outFile, []byte(output), 0644); err != nil {
			t.Skip("could not write output file")
		}
		script := `cat ` + outFile
		if exitCode != 0 {
			script += `; exit ` + string(rune('0'+exitCode))
		}
		writeScript(t, bin, script)

		// Clamp expected to valid UTF-8 (the checker uses strings.Contains).
		if !utf8.ValidString(expected) {
			expected = strings.ToValidUTF8(expected, "?")
		}

		hc := VersionHealthChecker{Expected: expected}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		err := hc.Check(ctx, bin) // must never panic

		trimmed := strings.TrimSpace(output)

		// Invariant 1: empty output must always be rejected.
		if trimmed == "" && exitCode == 0 && err == nil {
			t.Fatalf("empty output should be rejected, but Check returned nil")
		}

		// Invariant 2: non-zero exit code must always return an error.
		if exitCode != 0 && err == nil {
			t.Fatalf("non-zero exit code should be rejected, but Check returned nil")
		}

		// Invariant 3: if output contains "dev", Check must pass regardless of
		// Expected (dev/git builds bypass the version gate).
		if exitCode == 0 && trimmed != "" &&
			strings.Contains(trimmed, "dev") && expected != "" && err != nil {
			t.Fatalf("'dev' output should bypass version mismatch, but Check returned: %v", err)
		}

		// Invariant 4: if output contains expected (non-empty), Check must pass.
		if exitCode == 0 && trimmed != "" && expected != "" &&
			strings.Contains(trimmed, expected) && err != nil {
			t.Fatalf("output %q contains expected %q, but Check returned: %v", trimmed, expected, err)
		}
	})
}
