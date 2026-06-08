package testhelper

import (
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"encoding/pem"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// WriteServerCert extracts the self-signed certificate from a mock httptest.TLSServer
// and writes it to a PEM file. The testbin proxy can load this certificate to verify
// TLS connections without needing to resort to InsecureSkipVerify.
func WriteServerCert(server *httptest.Server, path string) error {
	cert := server.Certificate()
	pemBlock := &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: cert.Raw,
	}
	return os.WriteFile(path, pem.EncodeToMemory(pemBlock), 0644)
}

func ComputeSHA256(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

func ComputeSHA512(data []byte) string {
	hash := sha512.Sum512(data)
	return hex.EncodeToString(hash[:])
}

func SetupPrefix(t *testing.T, tmpDir string) string {
	t.Helper()
	prefix := filepath.Join(tmpDir, "prefix")
	coreTapDir := filepath.Join(prefix, "Taps", "homegrew", "homegrew-taps")
	if err := os.MkdirAll(coreTapDir, 0755); err != nil {
		t.Fatalf("failed to create core tap dir: %v", err)
	}
	// Initialize a real git repo so EnsureCloned sees it as already cloned.
	cmd := exec.Command("git", "init", "-b", "main")
	cmd.Dir = coreTapDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to init git in core tap: %v, output: %s", err, string(out))
	}
	return prefix
}

func GetProjectRoot(t *testing.T) string {
	t.Helper()
	cwd, errCwd := os.Getwd()
	if errCwd != nil {
		t.Skip("could not get current working directory")
	}

	root := cwd
	for {
		if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
			break
		}
		parent := filepath.Dir(root)
		if parent == root {
			t.Fatal("could not find project root")
		}
		root = parent
	}
	return root
}

func BuildTestBinary(t *testing.T, tmpDir string) string {
	t.Helper()
	exePath := filepath.Join(tmpDir, "grew-test")
	root := GetProjectRoot(t)

	cmdBuild := exec.Command("go", "build", "-tags=devmode", "-o", exePath, filepath.Join(root, "tests", "testbin", "main.go"))
	if out, err := cmdBuild.CombinedOutput(); err != nil {
		t.Fatalf("failed to build test binary: %v, output: %s", err, string(out))
	}
	return exePath
}

func CreateFormula(t *testing.T, prefix, name, yamlContent string) {
	t.Helper()
	coreTapDir := filepath.Join(prefix, "Taps", "homegrew", "homegrew-taps", "core")
	if err := os.MkdirAll(coreTapDir, 0755); err != nil {
		t.Fatalf("failed to create core tap dir: %v", err)
	}
	formulaPath := filepath.Join(coreTapDir, name+".yaml")
	if err := os.WriteFile(formulaPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("failed to write formula yaml: %v", err)
	}
}
