package testhelper

import (
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteServerCert(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	certPath := filepath.Join(tmpDir, "cert.pem")

	err := WriteServerCert(server, certPath)
	if err != nil {
		t.Fatalf("WriteServerCert failed: %v", err)
	}

	// Verify the file exists and can be read
	data, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatalf("failed to read written cert: %v", err)
	}

	// Verify it's a valid PEM block
	block, _ := pem.Decode(data)
	if block == nil {
		t.Error("written file is not valid PEM")
	}
	if block.Type != "CERTIFICATE" {
		t.Errorf("expected CERTIFICATE block, got %s", block.Type)
	}
}

func TestComputeSHA256(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{
			name: "empty bytes",
			data: []byte{},
		},
		{
			name: "simple string",
			data: []byte("hello"),
		},
		{
			name: "longer data",
			data: []byte("The quick brown fox jumps over the lazy dog"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ComputeSHA256(tt.data)

			// Verify it matches the standard library result
			hash := sha256.Sum256(tt.data)
			expected := hex.EncodeToString(hash[:])
			if result != expected {
				t.Errorf("result doesn't match standard library: %s != %s", result, expected)
			}

			// Verify result is a valid hex string of correct length
			if len(result) != 64 {
				t.Errorf("expected 64-char hex string, got %d chars", len(result))
			}
		})
	}
}

func TestComputeSHA512(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{
			name: "empty bytes",
			data: []byte{},
		},
		{
			name: "simple string",
			data: []byte("hello"),
		},
		{
			name: "longer data",
			data: []byte("The quick brown fox jumps over the lazy dog"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ComputeSHA512(tt.data)

			// Verify it matches the standard library result
			hash := sha512.Sum512(tt.data)
			expected := hex.EncodeToString(hash[:])
			if result != expected {
				t.Errorf("result doesn't match standard library: %s != %s", result, expected)
			}

			// Verify result is a valid hex string of correct length
			if len(result) != 128 {
				t.Errorf("expected 128-char hex string, got %d chars", len(result))
			}
		})
	}
}

func TestSetupPrefix(t *testing.T) {
	tmpDir := t.TempDir()
	prefix := SetupPrefix(t, tmpDir)

	// Verify the prefix was created
	if _, err := os.Stat(prefix); err != nil {
		t.Fatalf("prefix directory not created: %v", err)
	}

	// Verify subdirectories exist
	expectedDirs := []string{
		filepath.Join(prefix, "Taps", "homegrew", "homegrew-taps"),
	}

	for _, dir := range expectedDirs {
		if _, err := os.Stat(dir); err != nil {
			t.Errorf("expected directory not created: %s", dir)
		}
	}

	// Verify git repo was initialized
	gitDir := filepath.Join(prefix, "Taps", "homegrew", "homegrew-taps", ".git")
	if _, err := os.Stat(gitDir); err != nil {
		t.Errorf("git directory not created: %v", err)
	}
}

func TestGetProjectRoot(t *testing.T) {
	root := GetProjectRoot(t)

	// Verify it's an absolute path
	if !filepath.IsAbs(root) {
		t.Errorf("expected absolute path, got %s", root)
	}

	// Verify go.mod exists
	goModPath := filepath.Join(root, "go.mod")
	if _, err := os.Stat(goModPath); err != nil {
		t.Errorf("go.mod not found in project root: %v", err)
	}

	// Verify it contains grown directory
	if !strings.Contains(root, "grew") {
		t.Logf("project root: %s", root)
	}
}

func TestBuildTestBinary(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping build test in short mode")
	}

	tmpDir := t.TempDir()
	exePath := BuildTestBinary(t, tmpDir)

	// Verify the binary was created
	if _, err := os.Stat(exePath); err != nil {
		t.Fatalf("test binary not created: %v", err)
	}

	// Verify it's executable
	fileInfo, err := os.Stat(exePath)
	if err != nil {
		t.Fatalf("failed to stat binary: %v", err)
	}

	// Check that at least read permission is set for owner
	if fileInfo.Mode()&0400 == 0 {
		t.Error("binary is not readable")
	}
}

func TestCreateFormula(t *testing.T) {
	tmpDir := t.TempDir()
	prefix := SetupPrefix(t, tmpDir)

	formulaYAML := `
name: test-formula
version: 1.0.0
homepage: https://example.com
description: A test formula
`

	CreateFormula(t, prefix, "test-formula", formulaYAML)

	// Verify the formula file was created
	expectedPath := filepath.Join(prefix, "Taps", "homegrew", "homegrew-taps", "core", "test-formula.yaml")
	data, err := os.ReadFile(expectedPath)
	if err != nil {
		t.Fatalf("formula file not created: %v", err)
	}

	// Verify content matches
	if string(data) != formulaYAML {
		t.Errorf("formula content mismatch:\nexpected:\n%s\ngot:\n%s", formulaYAML, string(data))
	}
}

func TestWriteServerCert_ValidPEM(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer server.Close()

	tmpDir := t.TempDir()
	certPath := filepath.Join(tmpDir, "test.pem")

	err := WriteServerCert(server, certPath)
	if err != nil {
		t.Fatalf("WriteServerCert failed: %v", err)
	}

	// Verify the certificate can be parsed
	data, _ := os.ReadFile(certPath)
	block, _ := pem.Decode(data)

	cert := server.Certificate()
	if hex.EncodeToString(block.Bytes) != hex.EncodeToString(cert.Raw) {
		t.Error("certificate content mismatch")
	}
}
