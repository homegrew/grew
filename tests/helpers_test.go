package tests

import (
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"encoding/pem"
	"net/http/httptest"
	"os"
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
