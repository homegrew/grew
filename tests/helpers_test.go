//go:build integration

package tests

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

// writeServerCert extracts the self-signed certificate from a mock httptest.TLSServer
// and writes it to a PEM file. The testbin proxy can load this certificate to verify
// TLS connections without needing to resort to InsecureSkipVerify.
func writeServerCert(server *httptest.Server, path string) error {
	cert := server.Certificate()
	pemBlock := &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: cert.Raw,
	}
	return os.WriteFile(path, pem.EncodeToMemory(pemBlock), 0644)
}

func computeSHA256(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

func computeSHA512(data []byte) string {
	hash := sha512.Sum512(data)
	return hex.EncodeToString(hash[:])
}

func setupPrefix(t *testing.T, tmpDir string) string {
	prefix := filepath.Join(tmpDir, "prefix")
	if err := os.MkdirAll(filepath.Join(prefix, "Taps", ".git"), 0755); err != nil {
		t.Fatalf("failed to create fake .git dir: %v", err)
	}
	return prefix
}

func getProjectRoot(t *testing.T) string {
	cwd, _ := os.Getwd()
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

func buildTestBinary(t *testing.T, tmpDir string) string {
	exePath := filepath.Join(tmpDir, "grew-test")
	root := getProjectRoot(t)

	cmdBuild := exec.Command("go", "build", "-tags=devmode", "-o", exePath, filepath.Join(root, "tests", "testbin", "main.go"))
	if out, err := cmdBuild.CombinedOutput(); err != nil {
		t.Fatalf("failed to build test binary: %v, output: %s", err, string(out))
	}
	return exePath
}

func createFormula(t *testing.T, prefix, name, yamlContent string) {
	coreTapDir := filepath.Join(prefix, "Taps", "core")
	if err := os.MkdirAll(coreTapDir, 0755); err != nil {
		t.Fatalf("failed to create core tap dir: %v", err)
	}
	formulaPath := filepath.Join(coreTapDir, name+".yaml")
	if err := os.WriteFile(formulaPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("failed to write formula yaml: %v", err)
	}
}
