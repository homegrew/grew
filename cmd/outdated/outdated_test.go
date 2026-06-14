package outdated_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/homegrew/grew/pkg/receipt"
	"github.com/homegrew/grew/pkg/snapshot"
	"github.com/homegrew/grew/pkg/testhelper"
)

// platformKey returns the GOOS_GOARCH string used in formula YAML keys.
func platformKey() string {
	return runtime.GOOS + "_" + runtime.GOARCH
}

// simulateInstall creates a keg directory under <prefix>/Cellar/<name>/<version>/
// and writes a minimal .MANIFEST.json and INSTALL_RECEIPT.json so that grew
// treats the package as installed without performing an actual network install.
func simulateInstall(t *testing.T, prefix, name, version string, installedOnRequest bool) {
	t.Helper()

	kegPath := filepath.Join(prefix, "Cellar", name, version)
	if err := os.MkdirAll(kegPath, 0755); err != nil {
		t.Fatalf("simulateInstall: create keg dir %q: %v", kegPath, err)
	}

	m := &snapshot.Manifest{
		Name:               name,
		Version:            version,
		Platform:           platformKey(),
		InstalledAt:        snapshot.Now(),
		DownloadURL:        "https://example.com/" + name + ".tar.gz",
		DownloadSHA256:     strings.Repeat("a", 64),
		KegSHA256:          strings.Repeat("b", 64),
		InstalledOnRequest: installedOnRequest,
	}
	if err := snapshot.Save(m, kegPath); err != nil {
		t.Fatalf("simulateInstall: write manifest for %s@%s: %v", name, version, err)
	}

	r := &receipt.Receipt{
		Name:               name,
		Version:            version,
		PouredFromBottle:   true,
		InstalledAt:        time.Now().UTC(),
		InstalledOnRequest: installedOnRequest,
	}
	if err := receipt.Save(r, kegPath); err != nil {
		t.Fatalf("simulateInstall: write receipt for %s@%s: %v", name, version, err)
	}
}

// buildEnv returns the environment slice used to point grew at the test prefix.
func buildEnv(prefix, tmpDir string) []string {
	return append(os.Environ(),
		"HOMEGREW_PREFIX="+prefix,
		"HOMEGREW_CACHE="+filepath.Join(tmpDir, "cache"),
	)
}

// TestOutdated_NoPackages verifies that an empty prefix produces exit 0 and
// does not crash. The output should be empty or contain an up-to-date message.
func TestOutdated_NoPackages(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	exePath := testhelper.BuildTestBinary(t, tmpDir)
	prefix := testhelper.SetupPrefix(t, tmpDir)
	env := buildEnv(prefix, tmpDir)

	cmd := exec.Command(exePath, "outdated")
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("outdated on empty prefix failed: %v\nOutput: %s", err, string(out))
	}
	// Either silent or prints up-to-date; neither case should crash.
	output := strings.TrimSpace(string(out))
	if output != "" && !strings.Contains(output, "up-to-date") {
		t.Errorf("unexpected output for empty prefix: %q", output)
	}
}

// TestOutdated_DetectsOutdatedFormula simulates a formula installed at 1.0.0
// while the tap declares 2.0.0, and checks that the upgrade path appears.
func TestOutdated_DetectsOutdatedFormula(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	exePath := testhelper.BuildTestBinary(t, tmpDir)
	prefix := testhelper.SetupPrefix(t, tmpDir)

	// Tap declares version 2.0.0.
	testhelper.CreateFormula(t, prefix, "outpkg", `
name: outpkg
version: 2.0.0
url:
  `+platformKey()+`: https://example.com/outpkg.tar.gz
install:
  type: binary
  binary_name: outpkg
`)

	// But only 1.0.0 is installed on disk.
	simulateInstall(t, prefix, "outpkg", "1.0.0", true)

	env := buildEnv(prefix, tmpDir)

	cmd := exec.Command(exePath, "outdated")
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("outdated failed: %v\nOutput: %s", err, string(out))
	}
	output := string(out)
	if !strings.Contains(output, "outpkg") {
		t.Errorf("expected 'outpkg' in output, got: %s", output)
	}
	if !strings.Contains(output, "1.0.0") {
		t.Errorf("expected installed version '1.0.0' in output, got: %s", output)
	}
	if !strings.Contains(output, "2.0.0") {
		t.Errorf("expected available version '2.0.0' in output, got: %s", output)
	}
	// Homebrew-style output: "name (installed -> available)".
	if !strings.Contains(output, "->") {
		t.Errorf("expected version arrow '->' in output, got: %s", output)
	}
}

// TestOutdated_UpToDate verifies that a formula whose installed version matches
// the tap version is not reported as outdated.
func TestOutdated_UpToDate(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	exePath := testhelper.BuildTestBinary(t, tmpDir)
	prefix := testhelper.SetupPrefix(t, tmpDir)

	testhelper.CreateFormula(t, prefix, "current", `
name: current
version: 3.0.0
url:
  `+platformKey()+`: https://example.com/current.tar.gz
install:
  type: binary
  binary_name: current
`)

	simulateInstall(t, prefix, "current", "3.0.0", true)

	env := buildEnv(prefix, tmpDir)

	cmd := exec.Command(exePath, "outdated")
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("outdated failed: %v\nOutput: %s", err, string(out))
	}
	output := strings.TrimSpace(string(out))
	// Must not list a package that is current.
	if strings.Contains(output, "current") && strings.Contains(output, "->") {
		t.Errorf("up-to-date formula should not appear in outdated output, got: %s", output)
	}
	// Output should be empty or carry an up-to-date message.
	if output != "" && !strings.Contains(output, "up-to-date") {
		t.Errorf("unexpected output when everything is current: %q", output)
	}
}

// TestOutdated_QuietFlag verifies that --quiet prints only the package name
// and omits the version arrow.
func TestOutdated_QuietFlag(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	exePath := testhelper.BuildTestBinary(t, tmpDir)
	prefix := testhelper.SetupPrefix(t, tmpDir)

	testhelper.CreateFormula(t, prefix, "quietpkg", `
name: quietpkg
version: 5.0.0
url:
  `+platformKey()+`: https://example.com/quietpkg.tar.gz
install:
  type: binary
  binary_name: quietpkg
`)

	simulateInstall(t, prefix, "quietpkg", "4.0.0", true)

	env := buildEnv(prefix, tmpDir)

	cmd := exec.Command(exePath, "outdated", "--quiet")
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("outdated --quiet failed: %v\nOutput: %s", err, string(out))
	}
	output := strings.TrimSpace(string(out))
	if !strings.Contains(output, "quietpkg") {
		t.Errorf("expected 'quietpkg' in quiet output, got: %q", output)
	}
	if strings.Contains(output, "->") {
		t.Errorf("quiet output should not contain version arrows, got: %q", output)
	}
}

// TestOutdated_JSONFlag verifies that --json produces valid JSON containing a
// top-level "formulae" key.
func TestOutdated_JSONFlag(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	exePath := testhelper.BuildTestBinary(t, tmpDir)
	prefix := testhelper.SetupPrefix(t, tmpDir)

	testhelper.CreateFormula(t, prefix, "jsonpkg", `
name: jsonpkg
version: 9.0.0
url:
  `+platformKey()+`: https://example.com/jsonpkg.tar.gz
install:
  type: binary
  binary_name: jsonpkg
`)

	simulateInstall(t, prefix, "jsonpkg", "8.0.0", true)

	env := buildEnv(prefix, tmpDir)

	cmd := exec.Command(exePath, "outdated", "--json")
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("outdated --json failed: %v\nOutput: %s", err, string(out))
	}

	var parsed map[string]json.RawMessage
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("outdated --json did not produce valid JSON: %v\nOutput: %s", err, string(out))
	}
	if _, ok := parsed["formulae"]; !ok {
		t.Errorf("JSON output missing 'formulae' key; got keys: %v", keysOf(parsed))
	}

	// The formulae array should contain an entry for jsonpkg.
	var formulae []json.RawMessage
	if err := json.Unmarshal(parsed["formulae"], &formulae); err != nil {
		t.Fatalf("could not parse 'formulae' array: %v", err)
	}
	if len(formulae) == 0 {
		t.Fatalf("expected at least one entry in 'formulae', got empty array")
	}

	// Each entry should have installed_versions and current_version keys.
	var entry map[string]json.RawMessage
	if err := json.Unmarshal(formulae[0], &entry); err != nil {
		t.Fatalf("could not parse first formulae entry: %v", err)
	}
	for _, key := range []string{"installed_versions", "current_version"} {
		if _, ok := entry[key]; !ok {
			t.Errorf("formulae entry missing key %q; got keys: %v", key, keysOf(entry))
		}
	}
}

// TestOutdated_FormulaFilter verifies that --formula flag works without error
// even when no casks are installed.
func TestOutdated_FormulaFilter(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	exePath := testhelper.BuildTestBinary(t, tmpDir)
	prefix := testhelper.SetupPrefix(t, tmpDir)

	testhelper.CreateFormula(t, prefix, "filterpkg", `
name: filterpkg
version: 2.0.0
url:
  `+platformKey()+`: https://example.com/filterpkg.tar.gz
install:
  type: binary
  binary_name: filterpkg
`)

	simulateInstall(t, prefix, "filterpkg", "1.0.0", true)

	env := buildEnv(prefix, tmpDir)

	cmd := exec.Command(exePath, "outdated", "--formula")
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("outdated --formula failed: %v\nOutput: %s", err, string(out))
	}
	output := string(out)
	if !strings.Contains(output, "filterpkg") {
		t.Errorf("expected 'filterpkg' in --formula output, got: %s", output)
	}
}

// TestOutdated_Help verifies that --help exits 0 and the help text mentions
// all expected flags.
func TestOutdated_Help(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	exePath := testhelper.BuildTestBinary(t, tmpDir)
	prefix := testhelper.SetupPrefix(t, tmpDir)
	env := buildEnv(prefix, tmpDir)

	cmd := exec.Command(exePath, "outdated", "--help")
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("outdated --help failed: %v\nOutput: %s", err, string(out))
	}
	for _, flag := range []string{"--formula", "--cask", "--json", "--minimum-version"} {
		if !strings.Contains(string(out), flag) {
			t.Errorf("expected --help to contain %q; output:\n%s", flag, string(out))
		}
	}
}

// TestOutdated_MinimumVersion verifies that --minimum-version filters entries
// based on the installed version, not the available version.
//
// Setup: two outdated formulas
//   - "highpkg": installed 3.0.0 → available 4.0.0  (installed >= minimum 2.0.0 → included)
//   - "lowpkg":  installed 1.0.0 → available 4.0.0  (installed <  minimum 2.0.0 → excluded)
func TestOutdated_MinimumVersion(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	exePath := testhelper.BuildTestBinary(t, tmpDir)
	prefix := testhelper.SetupPrefix(t, tmpDir)

	for _, pkg := range []struct{ name, tapVer, installedVer string }{
		{"highpkg", "4.0.0", "3.0.0"},
		{"lowpkg", "4.0.0", "1.0.0"},
	} {
		testhelper.CreateFormula(t, prefix, pkg.name, `
name: `+pkg.name+`
version: `+pkg.tapVer+`
url:
  `+platformKey()+`: https://example.com/`+pkg.name+`.tar.gz
install:
  type: binary
  binary_name: `+pkg.name+`
`)
		simulateInstall(t, prefix, pkg.name, pkg.installedVer, true)
	}

	env := buildEnv(prefix, tmpDir)

	cmd := exec.Command(exePath, "outdated", "--minimum-version", "2.0.0")
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("outdated --minimum-version failed: %v\nOutput: %s", err, string(out))
	}
	output := string(out)

	// highpkg installed at 3.0.0 >= 2.0.0 → must appear
	if !strings.Contains(output, "highpkg") {
		t.Errorf("expected 'highpkg' (installed 3.0.0 >= minimum 2.0.0) in output, got: %s", output)
	}
	// lowpkg installed at 1.0.0 < 2.0.0 → must be filtered out
	if strings.Contains(output, "lowpkg") {
		t.Errorf("expected 'lowpkg' (installed 1.0.0 < minimum 2.0.0) to be filtered from output, got: %s", output)
	}
}

// keysOf is a small helper that extracts the keys of a map for error messages.
func keysOf[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
