package tests

import (
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
