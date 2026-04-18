package main

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"

	"github.com/homegrew/grew/internal/cmd"
)

func main() {
	if len(os.Args) < 2 {
		os.Exit(1)
	}

	// If a test certificate is provided, append it to the system pool so we can
	// strictly verify the mock TLS server without using InsecureSkipVerify.
	if certFile := os.Getenv("HOMEGREW_TEST_CERT_FILE"); certFile != "" {
		certPEM, err := os.ReadFile(certFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to read test cert: %v\n", err)
			os.Exit(1)
		}
		pool, err := x509.SystemCertPool()
		if err != nil || pool == nil {
			pool = x509.NewCertPool()
		}
		pool.AppendCertsFromPEM(certPEM)
		
		// Create a custom transport that uses the augmented root CA pool.
		// We make a copy of the DefaultTransport to preserve its other settings (like proxies, timeouts).
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.TLSClientConfig = &tls.Config{RootCAs: pool}
		http.DefaultTransport = transport
	}

	switch os.Args[1] {
	case "run":
		if err := cmd.RunSelfUpdate(nil); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	case "from-release":
		exePath, _ := os.Executable()
		if err := cmd.SelfUpdateFromRelease(exePath); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	default:
		// Delegate everything else (like "install", "_extract") to the real command router
		if err := cmd.Run(os.Args[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	}
}
