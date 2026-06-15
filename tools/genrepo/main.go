// Command genrepo is a unified tool for importing Homebrew formulas and casks
// into grew-compatible YAML definitions.
//
// Usage:
//
//	go run tools/genrepo/main.go formula [output_dir]
//	go run tools/genrepo/main.go cask [output_dir]
package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/homegrew/grew/pkg/homebrew"
	"github.com/homegrew/grew/pkg/logger"
)

func main() {
	fs := flag.NewFlagSet("genrepo", flag.ExitOnError)
	var verbose, debug bool
	fs.BoolVar(&verbose, "v", false, "Verbose output")
	fs.BoolVar(&debug, "debug", false, "Debug output (implies verbose)")

	fs.Usage = usage
	if err := fs.Parse(os.Args[1:]); err != nil {
		usage()
	}

	if debug {
		verbose = true
	}

	logger.Init(verbose, debug, false)

	args := fs.Args()
	if len(args) < 1 {
		usage()
	}

	cmd := args[0]
	cmdArgs := args[1:]

	switch cmd {
	case "formula":
		runFormulaImport(cmdArgs)
	case "cask":
		runCaskImport(cmdArgs)
	default:
		slog.Error("Unknown command", "command", cmd)
		usage()
	}
}

func usage() {
	fmt.Println("Usage: genrepo <command> [args]")
	fmt.Println("\nCommands:")
	fmt.Println("  formula [output_dir]    Import Homebrew formulas (default: core)")
	fmt.Println("  cask [output_dir]       Import Homebrew casks (default: cask)")
	os.Exit(1)
}

func getSubdir(name string) string {
	if len(name) == 0 {
		return "other"
	}
	firstChar := strings.ToLower(string(name[0]))
	if firstChar >= "a" && firstChar <= "z" {
		return firstChar
	}
	return "numeric"
}

func runFormulaImport(args []string) {
	outDirInput := "core"
	if len(args) > 0 {
		outDirInput = args[0]
	}

	outDir, err := safeOutputDir(outDirInput)
	if err != nil {
		slog.Error("Invalid output directory", "error", err, "outDir", outDirInput)
		os.Exit(1)
	}

	slog.Info("Fetching formulae from Homebrew API...")
	formulas, err := homebrew.FetchAllFormulae()
	if err != nil {
		slog.Error("Failed to fetch formulae", "error", err)
		os.Exit(1)
	}

	imported, skipped := 0, 0
	for _, f := range formulas {
		subdir := getSubdir(f.Name)
		targetDir := filepath.Join(outDir, subdir)

		if err := os.MkdirAll(targetDir, 0755); err != nil {
			slog.Error("Failed to create subdirectory", "dir", targetDir, "error", err)
			skipped++
			continue
		}

		outPath, err := safeJoinUnderBase(targetDir, f.Name+".yaml")
		if err != nil {
			slog.Warn("Skipping formula due to invalid output path", "name", f.Name, "error", err)
			skipped++
			continue
		}

		applyFormulaOverrides(f)

		if err := f.Validate(); err != nil {
			slog.Warn("Skipping invalid formula", "name", f.Name, "error", err)
			skipped++
			continue
		}

		saveYAML(outPath, f)
		imported++
	}
	slog.Info("Formula import complete", "imported", imported, "skipped", skipped)
}

func runCaskImport(args []string) {
	outDirInput := "cask"
	if len(args) > 0 {
		outDirInput = args[0]
	}

	outDir, err := safeOutputDir(outDirInput)
	if err != nil {
		slog.Error("Invalid output directory", "error", err, "outDir", outDirInput)
		os.Exit(1)
	}

	slog.Info("Fetching casks from Homebrew API...")
	casks, err := homebrew.FetchAllCasks()
	if err != nil {
		slog.Error("Failed to fetch casks", "error", err)
		os.Exit(1)
	}

	imported, skipped := 0, 0
	for _, c := range casks {
		subdir := getSubdir(c.Name)
		targetDir := filepath.Join(outDir, subdir)

		if err := os.MkdirAll(targetDir, 0755); err != nil {
			slog.Error("Failed to create subdirectory", "dir", targetDir, "error", err)
			skipped++
			continue
		}

		outPath, err := safeJoinUnderBase(targetDir, c.Name+".yaml")
		if err != nil {
			slog.Warn("Skipping cask due to invalid output path", "name", c.Name, "error", err)
			skipped++
			continue
		}

		if err := c.Validate(); err != nil {
			slog.Warn("Skipping invalid cask", "name", c.Name, "error", err)
			skipped++
			continue
		}

		saveYAML(outPath, c)
		imported++
	}

	slog.Info("Cask import complete", "imported", imported, "skipped", skipped)
}

func safeOutputDir(input string) (string, error) {
	if strings.TrimSpace(input) == "" {
		return "", fmt.Errorf("output directory cannot be empty")
	}

	baseDir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("resolve working directory: %w", err)
	}
	baseDir, err = filepath.Abs(baseDir)
	if err != nil {
		return "", fmt.Errorf("resolve absolute working directory: %w", err)
	}

	targetPath := filepath.Clean(input)
	targetAbs, err := filepath.Abs(filepath.Join(baseDir, targetPath))
	if err != nil {
		return "", fmt.Errorf("resolve output directory: %w", err)
	}

	rel, err := filepath.Rel(baseDir, targetAbs)
	if err != nil {
		return "", fmt.Errorf("check output directory: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("output directory must be within %s", baseDir)
	}

	return targetAbs, nil
}

func safeJoinUnderBase(base, rel string) (string, error) {
	baseAbs, err := filepath.Abs(base)
	if err != nil {
		return "", err
	}
	candidate := filepath.Clean(filepath.Join(baseAbs, rel))
	relativeToBase, err := filepath.Rel(baseAbs, candidate)
	if err != nil {
		return "", err
	}
	if relativeToBase == ".." || strings.HasPrefix(relativeToBase, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes base directory")
	}
	return candidate, nil
}

func saveYAML(path string, v any) {
	data, err := yaml.Marshal(v)
	if err != nil {
		slog.Warn("Marshal failed", "path", path, "error", err)
		return
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		slog.Warn("Write failed", "path", path, "error", err)
	}
}
