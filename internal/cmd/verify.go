package cmd

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"

	"github.com/homegrew/grew/internal/cellar"
	"github.com/homegrew/grew/internal/config"
	"github.com/homegrew/grew/internal/snapshot"
)

func runVerify(args []string) error {
	jsonOutput := false
	var targets []string

	for _, a := range args {
		if a == "--json" {
			jsonOutput = true
		} else {
			targets = append(targets, a)
		}
	}

	paths := config.Default()
	cel := &cellar.Cellar{Path: paths.Cellar}

	// If no targets, verify all installed.
	if len(targets) == 0 {
		pkgs, err := cel.List()
		if err != nil {
			return err
		}
		if len(pkgs) == 0 {
			fmt.Println("No packages installed.")
			return nil
		}
		for _, p := range pkgs {
			targets = append(targets, p.Name)
		}
	}

	allOK := true
	var jsonResults []map[string]any

	for _, name := range targets {
		if !cel.IsInstalled(name) {
			slog.Warn(name + " is not installed, skipping")
			continue
		}

		ver, err := cel.InstalledVersion(name)
		if err != nil {
			slog.Warn(fmt.Sprintf("%s: %v", name, err))
			continue
		}
		kegPath, err := cel.KegPath(name, ver)
		if err != nil {
			slog.Warn(fmt.Sprintf("%s: %v", name, err))
			continue
		}

		if !snapshot.Exists(kegPath) {
			msg := fmt.Sprintf("%s %s: no manifest (installed before snapshotting was enabled)", name, ver)
			if jsonOutput {
				jsonResults = append(jsonResults, map[string]any{"name": name, "version": ver, "status": "no_manifest"})
			} else {
				fmt.Println(msg)
			}
			continue
		}

		result, err := snapshot.Verify(kegPath)
		if err != nil {
			slog.Error(fmt.Sprintf("verifying %s: %v", name, err))
			allOK = false
			continue
		}

		if jsonOutput {
			jsonResults = append(jsonResults, map[string]any{
				"name": result.Name, "version": result.Version,
				"ok": result.OK, "missing": result.Missing,
				"modified": result.Modified, "added": result.Added,
				"errors": result.Errors,
				"keg_sha256_mismatch": result.KegSHA256Mismatch,
				"keg_sha512_mismatch": result.KegSHA512Mismatch,
			})
		} else if result.OK {
			fmt.Printf("%s %s: OK\n", result.Name, result.Version)
		} else {
			allOK = false
			fmt.Printf("%s %s: FAILED\n", result.Name, result.Version)
			for _, f := range result.Missing {
				fmt.Printf("  missing:  %s\n", f)
			}
			for _, f := range result.Modified {
				fmt.Printf("  modified: %s\n", f)
			}
			for _, f := range result.Added {
				fmt.Printf("  added:    %s\n", f)
			}
			if result.KegSHA256Mismatch {
				fmt.Printf("  error:    aggregate SHA256 mismatch\n")
			}
			if result.KegSHA512Mismatch {
				fmt.Printf("  error:    aggregate SHA512 mismatch\n")
			}
			for _, e := range result.Errors {
				fmt.Printf("  error:    %s\n", e)
			}
		}
	}

	if jsonOutput {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(jsonResults)
	}

	if !allOK {
		return fmt.Errorf("verification failed for one or more packages")
	}
	return nil
}
