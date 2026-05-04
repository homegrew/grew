package verify

import "github.com/homegrew/grew/internal/cmd"

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"

	"github.com/homegrew/grew/internal/cellar"
	"github.com/homegrew/grew/pkg/snapshot"
	"github.com/spf13/cobra"
)

var verifyJsonFlag bool

var Command = &cobra.Command{
	Use:   "verify [formula ...]",
	Short: "Verify the integrity of installed packages",
	Long: `Verify the integrity of installed packages by comparing the filesystem
against the snapshot manifest recorded at install time.

With no arguments, verifies all installed packages.

Each package is checked for:
  - Missing files (deleted after install)
  - Modified files (content changed since install)
  - Added files (unexpected files appeared in the keg)

Exit code 0 if all packages pass, 1 if any discrepancies found.`,
	Example: `  grew verify
  grew verify jq
  grew verify --json`,
	RunE: func(c *cobra.Command, args []string) error {
		slog.Debug("starting verify command execution")
		ctx, err := cmd.NewReadContext()
		if err != nil {
			return err
		}

		packages, err := ctx.Cellar.List()
		if err != nil {
			return err
		}

		targets := args
		if len(targets) > 0 {
			targetSet := make(map[string]bool, len(targets))
			for _, t := range targets {
				targetSet[t] = true
			}
			var filtered []cellar.InstalledPackage
			for _, p := range packages {
				if targetSet[p.Name] {
					filtered = append(filtered, p)
				}
			}
			for _, t := range targets {
				found := false
				for _, p := range packages {
					if p.Name == t {
						found = true
						break
					}
				}
				if !found {
					if !verifyJsonFlag {
						slog.Warn(fmt.Sprintf("%s is not installed, skipping", t))
					}
				}
			}
			packages = filtered
		}

		if len(packages) == 0 {
			if !verifyJsonFlag {
				fmt.Println("No installed packages to verify.")
			}
			return nil
		}

		allOK := true
		jsonResults := make(map[string]any)

		for _, pkg := range packages {
			kegPath, err := ctx.Cellar.KegPath(pkg.Name, pkg.Version)
			if err != nil {
				continue
			}

			if !snapshot.Exists(kegPath) {
				if !verifyJsonFlag {
					fmt.Printf("%s: skipped (no manifest)\n", pkg.Name)
				}
				jsonResults[pkg.Name] = map[string]string{"status": "skipped", "reason": "no manifest"}
				continue
			}

			result, err := snapshot.Verify(kegPath)
			if err != nil {
				if !verifyJsonFlag {
					fmt.Printf("%s: error verifying manifest: %v\n", pkg.Name, err)
				}
				jsonResults[pkg.Name] = map[string]string{"status": "error", "error": err.Error()}
				allOK = false
				continue
			}

			if result.OK {
				if !verifyJsonFlag {
					fmt.Printf("%s: OK\n", pkg.Name)
				}
				jsonResults[pkg.Name] = map[string]string{"status": "ok"}
			} else {
				allOK = false
				jsonResults[pkg.Name] = result
				if !verifyJsonFlag {
					fmt.Printf("%s: FAILED\n", pkg.Name)
					for _, f := range result.Modified {
						fmt.Printf("  modified: %s\n", f)
					}
					for _, f := range result.Missing {
						fmt.Printf("  missing:  %s\n", f)
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
		}

		if verifyJsonFlag {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			enc.Encode(jsonResults)
		}

		if !allOK {
			return fmt.Errorf("verification failed for one or more packages")
		}
		return nil
	},
}

func init() {
	Command.Flags().BoolVar(&verifyJsonFlag, "json", false, "Output results as JSON for machine consumption")
}
