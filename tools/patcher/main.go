package main

import (
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/homegrew/grew/internal/flags"
	"github.com/homegrew/grew/internal/release"
	"github.com/homegrew/grew/pkg/validation"
	"github.com/homegrew/grew/pkg/ui"
)

type platform struct {
	os   string
	arch string
}

var platforms = []platform{
	{"Darwin", "x86_64"},
	{"Darwin", "arm64"},
}

func main() {
	// Parse global flags before the command if they exist
	args := flags.Parse(os.Args[1:])

	fs := flag.NewFlagSet("patcher", flag.ExitOnError)
	flags.Register(fs)

	var outputDir string
	fs.StringVar(&outputDir, "D", ".", "Output directory for generated files")

	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: %s [options] <previous_release> <new_release>\n", os.Args[0])
		fmt.Fprintln(fs.Output(), "\nOptions:")
		fs.PrintDefaults()
		fmt.Fprintln(fs.Output(), "\nExample: patcher -v -D dist/ v0.4.0 v0.4.1")
	}

	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}

	flags.Resolve()

	remainingArgs := fs.Args()
	if len(remainingArgs) != 2 {
		fs.Usage()
		os.Exit(1)
	}

	prevRelease := remainingArgs[0]
	newRelease := remainingArgs[1]

	logMsg := func(format string, a ...interface{}) {
		if flags.Verbose {
			ui.FprintArrow(os.Stdout, ""+format+"", a...)
		}
	}

	// Basic validation of version strings.
	if !validation.IsValidVersion(strings.TrimPrefix(prevRelease, "v")) || !validation.IsValidVersion(strings.TrimPrefix(newRelease, "v")) {
		slog.Error("Invalid version string provided.")
		os.Exit(1)
	}

	// Check if bsdiff is available
	if _, err := exec.LookPath("bsdiff"); err != nil {
		slog.Error("bsdiff command not found. Please install bsdiff to generate patches.")
		os.Exit(1)
	}

	// Ensure output directory exists
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		slog.Error("Failed to create output directory", "dir", outputDir, "err", err)
		os.Exit(1)
	}

	var binaryChecksums strings.Builder

	for _, p := range platforms {
		slog.Info("Processing platform", "os", p.os, "arch", p.arch)

		archiveName := fmt.Sprintf("grew_%s_%s.tar.gz", p.os, p.arch)
		rawBinName := fmt.Sprintf("grew_%s_%s", p.os, p.arch)

		oldURL := fmt.Sprintf("https://github.com/homegrew/grew/releases/download/%s/%s", prevRelease, archiveName)
		newURL := fmt.Sprintf("https://github.com/homegrew/grew/releases/download/%s/%s", newRelease, archiveName)

		// 1. Download and Extract Old Binary
		logMsg("Downloading old release %s", archiveName)
		oldTmp, err := release.DownloadTemp(oldURL)
		if err != nil {
			slog.Warn("Could not download old archive (skipping platform)", "url", oldURL, "err", err)
			continue
		}
		oldBinBytes, err := release.ExtractBinaryFromFile(oldTmp)
		os.Remove(oldTmp)
		if err != nil {
			slog.Error("Failed to extract old binary", "err", err)
			os.Exit(1)
		}
		oldBinFile := writeTempFile("oldBin", oldBinBytes)
		defer os.Remove(oldBinFile)

		// 2. Download and Extract New Binary
		logMsg("Downloading new release %s", archiveName)
		newTmp, err := release.DownloadTemp(newURL)
		if err != nil {
			slog.Error("Could not download new archive", "url", newURL, "err", err)
			os.Exit(1)
		}
		newBinBytes, err := release.ExtractBinaryFromFile(newTmp)
		os.Remove(newTmp)
		if err != nil {
			slog.Error("Failed to extract new binary", "err", err)
			os.Exit(1)
		}
		newBinFile := writeTempFile("newBin", newBinBytes)
		defer os.Remove(newBinFile)

		// 3. Compute Binary Checksums (SHA256 & SHA512) for the NEW binary
		h256 := sha256.New()
		h256.Write(newBinBytes)
		binSHA256 := hex.EncodeToString(h256.Sum(nil))

		h512 := sha512.New()
		h512.Write(newBinBytes)
		binSHA512 := hex.EncodeToString(h512.Sum(nil))

		binaryChecksums.WriteString(fmt.Sprintf("%s  %s\n", binSHA256, rawBinName))
		binaryChecksums.WriteString(fmt.Sprintf("%s  %s\n", binSHA512, rawBinName))

		// 4. Generate the Delta Patch using bsdiff
		patchFileName := fmt.Sprintf("grew_%s_%s_%s_to_%s.patch", p.os, p.arch, prevRelease, newRelease)
		patchFile := filepath.Join(outputDir, patchFileName)

		logMsg("Generating patch %s", patchFile)
		cmd := exec.Command("bsdiff", oldBinFile, newBinFile, patchFile)
		if flags.Verbose {
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
		}
		if err := cmd.Run(); err != nil {
			slog.Error("bsdiff failed", "err", err)
			os.Exit(1)
		}

		// 5. Compute and Save Patch Checksums
		patchSHA256, err := release.FileSHA256(patchFile)
		if err != nil {
			slog.Error("Failed to compute patch SHA256", "err", err)
			os.Exit(1)
		}
		patchSHA512, err := release.FileSHA512(patchFile)
		if err != nil {
			slog.Error("Failed to compute patch SHA512", "err", err)
			os.Exit(1)
		}

		sha256File := patchFile + ".sha256"
		os.WriteFile(sha256File, []byte(fmt.Sprintf("%s  %s\n", patchSHA256, patchFileName)), 0644)

		sha512File := patchFile + ".sha512"
		os.WriteFile(sha512File, []byte(fmt.Sprintf("%s  %s\n", patchSHA512, patchFileName)), 0644)

		logMsg("Success! Generated %s and its checksum files.", patchFile)
	}

	// 6. Write out the accumulated binary-checksums.txt
	if binaryChecksums.Len() > 0 {
		outBinFile := filepath.Join(outputDir, "binary-checksums.txt")
		if err := os.WriteFile(outBinFile, []byte(binaryChecksums.String()), 0644); err != nil {
			slog.Error("Failed to write output file", "err", err)
			os.Exit(1)
		}
		logMsg("Successfully generated %s", outBinFile)
	} else {
		slog.Warn("No platforms were processed successfully.")
	}
}

func writeTempFile(prefix string, data []byte) string {
	f, err := os.CreateTemp("", prefix+"-*")
	if err != nil {
		slog.Error("Failed to create temp file", "err", err)
		os.Exit(1)
	}
	defer f.Close()
	if _, err := f.Write(data); err != nil {
		slog.Error("Failed to write to temp file", "err", err)
		os.Exit(1)
	}
	return f.Name()
}
