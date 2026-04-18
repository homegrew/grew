package main

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"os"

	"github.com/homegrew/grew/internal/cmd"
)

func main() {
	if len(os.Args) < 2 {
		os.Exit(1)
	}

	// Globally skip TLS verification in the test binary so we can use
	// httptest.NewTLSServer with self-signed certificates.
	http.DefaultTransport.(*http.Transport).TLSClientConfig = &tls.Config{InsecureSkipVerify: true}

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
