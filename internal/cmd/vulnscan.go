package cmd

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/homegrew/grew/internal/cellar"
	"github.com/homegrew/grew/internal/config"
	"github.com/homegrew/grew/internal/formula"
	"github.com/homegrew/grew/internal/osvdev"
	"github.com/homegrew/grew/internal/signing"
	"github.com/homegrew/grew/internal/snapshot"
	"github.com/homegrew/grew/internal/tap"
	"github.com/homegrew/grew/pkg/validation"
)

// vulnSeverity represents the severity of a vulnerability finding.
type vulnSeverity string

const (
	severityCritical vulnSeverity = "critical"
	severityHigh     vulnSeverity = "high"
	severityMedium   vulnSeverity = "medium"
	severityLow      vulnSeverity = "low"
)

// vulnFinding represents a single vulnerability finding.
type vulnFinding struct {
	Package  string       `json:"package"`
	Version  string       `json:"version,omitempty"`
	Severity vulnSeverity `json:"severity"`
	Category string       `json:"category"`
	Detail   string       `json:"detail"`
}

func runVulnScan(args []string) error {
	fs := flag.NewFlagSet("vuln-scan", flag.ContinueOnError)
	jsonOutput := fs.Bool("json", false, "Output results as JSON")
	quiet := fs.Bool("quiet", false, "Only show critical and high severity findings")
	fs.BoolVar(quiet, "q", false, "Only show critical and high severity findings")
	skipOSV := fs.Bool("offline", false, "Skip OSV.dev vulnerability database query")
	if err := fs.Parse(args); err != nil {
		return err
	}
	targets := fs.Args()

	paths := config.Default()

	tapMgr := &tap.Manager{TapsDir: paths.Taps}
	if err := tapMgr.InitCore(); err != nil {
		Debugf("init core tap: %v\n", err)
	}

	loader := newLoader(paths.Taps)
	cel := &cellar.Cellar{Path: paths.Cellar}

	packages, err := cel.List()
	if err != nil {
		return fmt.Errorf("list installed packages: %w", err)
	}

	// Filter to targets if specified.
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
		// Report targets not found.
		for _, t := range targets {
			found := false
			for _, p := range packages {
				if p.Name == t {
					found = true
					break
				}
			}
			if !found {
				fmt.Fprintf(os.Stderr, "Warning: %s is not installed, skipping\n", t)
			}
		}
		packages = filtered
	}

	if len(packages) == 0 {
		fmt.Println("No installed packages to scan.")
		return nil
	}

	// Load all formulas for cross-referencing.
	allFormulas, _ := loader.LoadAll()
	formulaMap := make(map[string]*formula.Formula, len(allFormulas))
	for _, f := range allFormulas {
		formulaMap[f.Name] = f
	}

	if !*jsonOutput {
		fmt.Println("Scanning installed packages for vulnerabilities...")
	}

	var findings []vulnFinding

	// Local checks per package.
	for _, pkg := range packages {
		findings = append(findings, scanPackage(pkg, cel, paths, formulaMap)...)
	}

	// OSV.dev known vulnerability check.
	if !*skipOSV {
		findings = append(findings, scanOSV(packages, formulaMap, *jsonOutput)...)
	}

	// Global checks (not tied to a specific package).
	findings = append(findings, scanGlobalPermissions(paths)...)

	// Filter by severity if quiet.
	if *quiet {
		var filtered []vulnFinding
		for _, f := range findings {
			if f.Severity == severityCritical || f.Severity == severityHigh {
				filtered = append(filtered, f)
			}
		}
		findings = filtered
	}

	if *jsonOutput {
		return printVulnJSON(findings)
	}
	return printVulnText(findings)
}

// scanOSV queries the OSV.dev database for known vulnerabilities affecting
// installed packages, following the same approach as homebrew-brew-vulns:
// extract repo URL from formula source/download URLs and query via the GIT ecosystem.
func scanOSV(packages []cellar.InstalledPackage, formulaMap map[string]*formula.Formula, silent bool) []vulnFinding {
	// Build queries for packages that have identifiable git repo URLs.
	type queryInfo struct {
		pkg     cellar.InstalledPackage
		repoURL string
		version string
	}
	var queries []queryInfo

	for _, pkg := range packages {
		f, ok := formulaMap[pkg.Name]
		if !ok {
			continue
		}

		repoURL := extractRepoURL(f)
		if repoURL == "" {
			Debugf("vuln-scan: no repo URL for %s, skipping OSV check\n", pkg.Name)
			continue
		}

		version := extractVersionTag(f, pkg.Version)
		queries = append(queries, queryInfo{
			pkg:     pkg,
			repoURL: repoURL,
			version: version,
		})
	}

	if len(queries) == 0 {
		return nil
	}

	skipped := len(packages) - len(queries)
	if !silent {
		fmt.Printf("==> Querying OSV.dev for %d package(s)...\n", len(queries))
		if skipped > 0 {
			Logf("    (%d packages skipped — no supported source URL)\n", skipped)
		}
	}

	// Batch query OSV.
	client := osvdev.NewClient()
	batchInput := make([]osvdev.QueryPackage, len(queries))
	for i, q := range queries {
		batchInput[i] = osvdev.QueryPackage{
			RepoURL: q.repoURL,
			Version: q.version,
		}
		Debugf("vuln-scan: OSV query %s -> repo=%s version=%s\n", q.pkg.Name, q.repoURL, q.version)
	}

	batchResults, err := client.QueryBatch(batchInput)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: OSV.dev query failed: %v\n", err)
		return nil
	}

	var findings []vulnFinding

	for i, vulns := range batchResults {
		if len(vulns) == 0 {
			continue
		}

		q := queries[i]

		// Fetch full details for each vulnerability.
		for _, v := range vulns {
			full, err := client.GetVulnerability(v.ID)
			if err != nil {
				Debugf("vuln-scan: failed to fetch %s: %v\n", v.ID, err)
				full = &v // use the summary data from the batch response
			}

			sev := mapOSVSeverity(full.SeverityString())
			cves := full.CVEIDs()
			fixedVersions := full.FixedVersions()

			detail := full.ID
			if len(cves) > 0 {
				detail += " (" + strings.Join(cves, ", ") + ")"
			}
			if full.Summary != "" {
				summary := full.Summary
				if len(summary) > 120 {
					summary = summary[:120] + "..."
				}
				detail += " — " + summary
			}
			if len(fixedVersions) > 0 {
				detail += " [fixed in: " + strings.Join(fixedVersions, ", ") + "]"
			}
			if advisory := full.AdvisoryURL(); advisory != "" {
				detail += " " + advisory
			}

			findings = append(findings, vulnFinding{
				Package:  q.pkg.Name,
				Version:  q.pkg.Version,
				Severity: sev,
				Category: "cve",
				Detail:   detail,
			})
		}
	}

	return findings
}

// repoURLPattern matches GitHub, GitLab, and Codeberg repository URLs.
var repoURLPattern = regexp.MustCompile(
	`https?://(?:github\.com|gitlab\.com|codeberg\.org)/([^/]+/[^/]+?)(?:\.git)?(?:/|$)`,
)

// extractRepoURL extracts a git repository URL from a formula's URLs.
// It checks homepage, source URL, and download URLs for supported forges.
func extractRepoURL(f *formula.Formula) string {
	// Collect candidate URLs in priority order.
	var candidates []string

	// Source URL is the best signal (points to the actual source repo).
	if f.Source.URL != "" {
		candidates = append(candidates, f.Source.URL)
	}
	if f.SourceURL != "" {
		candidates = append(candidates, f.SourceURL)
	}

	// Download URLs often contain the repo path.
	for _, u := range f.URL {
		candidates = append(candidates, u)
	}
	for _, b := range f.Bottle {
		candidates = append(candidates, b.URL)
	}

	// Homepage sometimes points to the repo.
	if f.Homepage != "" {
		candidates = append(candidates, f.Homepage)
	}

	for _, rawURL := range candidates {
		m := repoURLPattern.FindStringSubmatch(rawURL)
		if m == nil {
			continue
		}
		// Reconstruct clean HTTPS repo URL.
		u, err := url.Parse(rawURL)
		if err != nil {
			continue
		}
		host := u.Host
		repo := m[1]
		// Remove common suffixes from repo path.
		repo = strings.TrimSuffix(repo, ".git")
		repo = strings.TrimSuffix(repo, "/")
		return "https://" + host + "/" + repo
	}

	return ""
}

// extractVersionTag builds a git version tag from the formula version.
// Tries common tag formats: "v1.2.3", "1.2.3", "<name>-1.2.3".
func extractVersionTag(f *formula.Formula, installedVersion string) string {
	// Try to extract tag from source URL first (most accurate).
	urls := []string{f.Source.URL, f.SourceURL}
	for _, u := range f.URL {
		urls = append(urls, u)
	}

	for _, rawURL := range urls {
		if rawURL == "" {
			continue
		}
		if tag := extractTagFromURL(rawURL); tag != "" {
			return tag
		}
	}

	// Fallback: use the installed version directly.
	return installedVersion
}

// tagFromURLPattern extracts version tags from common archive URL patterns.
var tagFromURLPatterns = []*regexp.Regexp{
	// GitHub/GitLab archive: /archive/refs/tags/v1.2.3.tar.gz
	regexp.MustCompile(`/archive/(?:refs/tags/)?([^/]+?)(?:\.tar\.gz|\.zip)$`),
	// GitHub releases: /releases/download/v1.2.3/...
	regexp.MustCompile(`/releases/download/([^/]+)/`),
	// Codeberg/generic: /archive/v1.2.3.tar.gz
	regexp.MustCompile(`/archive/([^/]+?)\.tar\.gz$`),
}

func extractTagFromURL(rawURL string) string {
	for _, pat := range tagFromURLPatterns {
		m := pat.FindStringSubmatch(rawURL)
		if m != nil {
			return m[1]
		}
	}
	return ""
}

// mapOSVSeverity maps an OSV severity string to our vulnSeverity type.
func mapOSVSeverity(sev string) vulnSeverity {
	switch sev {
	case "critical":
		return severityCritical
	case "high":
		return severityHigh
	case "medium":
		return severityMedium
	case "low":
		return severityLow
	default:
		return severityMedium // unknown defaults to medium
	}
}

// scanPackage runs all local vulnerability checks on a single installed package.
func scanPackage(
	pkg cellar.InstalledPackage,
	cel *cellar.Cellar,
	paths config.Paths,
	formulaMap map[string]*formula.Formula,
) []vulnFinding {
	var findings []vulnFinding

	kegPath, err := cel.KegPath(pkg.Name, pkg.Version)
	if err != nil {
		return findings
	}

	// 1. Manifest integrity check.
	findings = append(findings, checkManifestIntegrity(pkg, kegPath)...)

	// 2. Signature verification.
	findings = append(findings, checkSignatureStatus(pkg, kegPath, paths, formulaMap)...)

	// 3. Formula-level security checks.
	findings = append(findings, checkFormulaSecurity(pkg, formulaMap)...)

	// 4. File permission checks inside the keg.
	findings = append(findings, checkKegPermissions(pkg, kegPath)...)

	// 5. Outdated version check.
	findings = append(findings, checkOutdatedVersion(pkg, formulaMap)...)

	return findings
}

// checkManifestIntegrity verifies the package manifest against installed files.
func checkManifestIntegrity(pkg cellar.InstalledPackage, kegPath string) []vulnFinding {
	var findings []vulnFinding

	if !snapshot.Exists(kegPath) {
		findings = append(findings, vulnFinding{
			Package:  pkg.Name,
			Version:  pkg.Version,
			Severity: severityMedium,
			Category: "integrity",
			Detail:   "no manifest found — cannot verify file integrity (installed before snapshotting was enabled)",
		})
		return findings
	}

	result, err := snapshot.Verify(kegPath)
	if err != nil {
		findings = append(findings, vulnFinding{
			Package:  pkg.Name,
			Version:  pkg.Version,
			Severity: severityHigh,
			Category: "integrity",
			Detail:   fmt.Sprintf("manifest verification error: %v", err),
		})
		return findings
	}

	if result.OK {
		return findings
	}

	for _, f := range result.Modified {
		findings = append(findings, vulnFinding{
			Package:  pkg.Name,
			Version:  pkg.Version,
			Severity: severityCritical,
			Category: "integrity",
			Detail:   fmt.Sprintf("file modified since installation: %s", f),
		})
	}
	for _, f := range result.Missing {
		findings = append(findings, vulnFinding{
			Package:  pkg.Name,
			Version:  pkg.Version,
			Severity: severityHigh,
			Category: "integrity",
			Detail:   fmt.Sprintf("file missing since installation: %s", f),
		})
	}
	for _, f := range result.Added {
		findings = append(findings, vulnFinding{
			Package:  pkg.Name,
			Version:  pkg.Version,
			Severity: severityHigh,
			Category: "integrity",
			Detail:   fmt.Sprintf("unexpected file added to keg: %s", f),
		})
	}

	return findings
}

// checkSignatureStatus verifies the package was installed from a signed formula.
func checkSignatureStatus(pkg cellar.InstalledPackage, _ string, paths config.Paths, formulaMap map[string]*formula.Formula) []vulnFinding {
	var findings []vulnFinding

	trustedKeys, err := signing.LoadTrustedKeys(paths.Root)
	if err != nil {
		Debugf("load trusted keys for %s: %v\n", pkg.Name, err)
		return findings
	}
	if len(trustedKeys) == 0 {
		// No trust store configured — can't verify signatures.
		return findings
	}

	f, ok := formulaMap[pkg.Name]
	if !ok {
		findings = append(findings, vulnFinding{
			Package:  pkg.Name,
			Version:  pkg.Version,
			Severity: severityMedium,
			Category: "signature",
			Detail:   "formula definition not found in any tap — cannot verify signature",
		})
		return findings
	}

	sig := f.GetSignature()
	if sig == "" {
		findings = append(findings, vulnFinding{
			Package:  pkg.Name,
			Version:  pkg.Version,
			Severity: severityMedium,
			Category: "signature",
			Detail:   "formula has no bottle signature (trusted keys are configured)",
		})
		return findings
	}

	// Verify the signature against the formula's SHA256.
	sha, err := f.GetSHA256()
	if err != nil {
		Debugf("get sha256 for %s: %v\n", pkg.Name, err)
		return findings
	}

	if !signing.VerifyAny(trustedKeys, sha, sig) {
		findings = append(findings, vulnFinding{
			Package:  pkg.Name,
			Version:  pkg.Version,
			Severity: severityCritical,
			Category: "signature",
			Detail:   "bottle signature does not verify against any trusted key",
		})
	}

	return findings
}

// checkFormulaSecurity checks formula-level security properties.
func checkFormulaSecurity(pkg cellar.InstalledPackage, formulaMap map[string]*formula.Formula) []vulnFinding {
	var findings []vulnFinding

	f, ok := formulaMap[pkg.Name]
	if !ok {
		return findings
	}

	// Check for missing SHA256 hashes.
	key := formula.PlatformKey()
	if len(f.Bottle) > 0 {
		if b, ok := f.Bottle[key]; ok {
			if err := validation.ValidateSHA256(b.SHA256); err != nil {
				findings = append(findings, vulnFinding{
					Package:  pkg.Name,
					Version:  pkg.Version,
					Severity: severityHigh,
					Category: "checksum",
					Detail:   fmt.Sprintf("invalid bottle SHA256 for %s: %v", key, err),
				})
			}
		}
	} else if len(f.SHA256) > 0 {
		if hash, ok := f.SHA256[key]; ok {
			if err := validation.ValidateSHA256(hash); err != nil {
				findings = append(findings, vulnFinding{
					Package:  pkg.Name,
					Version:  pkg.Version,
					Severity: severityHigh,
					Category: "checksum",
					Detail:   fmt.Sprintf("invalid SHA256 for %s: %v", key, err),
				})
			}
		}
	}

	// Check for HTTP URLs.
	for platform, u := range f.URL {
		if !strings.HasPrefix(u, "https://") {
			findings = append(findings, vulnFinding{
				Package:  pkg.Name,
				Version:  pkg.Version,
				Severity: severityCritical,
				Category: "transport",
				Detail:   fmt.Sprintf("formula URL for %s uses insecure HTTP: %s", platform, u),
			})
		}
	}
	for platform, b := range f.Bottle {
		if !strings.HasPrefix(b.URL, "https://") {
			findings = append(findings, vulnFinding{
				Package:  pkg.Name,
				Version:  pkg.Version,
				Severity: severityCritical,
				Category: "transport",
				Detail:   fmt.Sprintf("bottle URL for %s uses insecure HTTP: %s", platform, b.URL),
			})
		}
	}

	return findings
}

// checkKegPermissions scans the keg directory for dangerous file permissions.
func checkKegPermissions(pkg cellar.InstalledPackage, kegPath string) []vulnFinding {
	var findings []vulnFinding

	err := filepath.Walk(kegPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip inaccessible files
		}

		// Skip the manifest file itself.
		if filepath.Base(path) == snapshot.ManifestFile {
			return nil
		}

		perm := info.Mode().Perm()
		mode := info.Mode()

		// World-writable files.
		if perm&0002 != 0 {
			relPath, _ := filepath.Rel(kegPath, path)
			findings = append(findings, vulnFinding{
				Package:  pkg.Name,
				Version:  pkg.Version,
				Severity: severityHigh,
				Category: "permissions",
				Detail:   fmt.Sprintf("world-writable file: %s (%04o)", relPath, perm),
			})
		}

		// Setuid/setgid bits on regular files.
		if mode.IsRegular() && (mode&os.ModeSetuid != 0 || mode&os.ModeSetgid != 0) {
			relPath, _ := filepath.Rel(kegPath, path)
			sev := severityCritical
			detail := fmt.Sprintf("setuid/setgid binary: %s (%s)", relPath, mode)
			findings = append(findings, vulnFinding{
				Package:  pkg.Name,
				Version:  pkg.Version,
				Severity: sev,
				Category: "permissions",
				Detail:   detail,
			})
		}

		return nil
	})
	if err != nil {
		Debugf("walk %s: %v\n", kegPath, err)
	}

	return findings
}

// checkOutdatedVersion reports if a newer version is available in the tap.
func checkOutdatedVersion(pkg cellar.InstalledPackage, formulaMap map[string]*formula.Formula) []vulnFinding {
	var findings []vulnFinding

	f, ok := formulaMap[pkg.Name]
	if !ok {
		return findings
	}

	if f.Version != pkg.Version {
		findings = append(findings, vulnFinding{
			Package:  pkg.Name,
			Version:  pkg.Version,
			Severity: severityLow,
			Category: "outdated",
			Detail:   fmt.Sprintf("installed version %s, latest available %s — may be missing security fixes", pkg.Version, f.Version),
		})
	}

	return findings
}

// scanGlobalPermissions checks grew prefix directories for dangerous permissions.
func scanGlobalPermissions(paths config.Paths) []vulnFinding {
	var findings []vulnFinding

	dirs := []struct {
		label string
		path  string
	}{
		{"prefix", paths.Root},
		{"Cellar", paths.Cellar},
		{"bin", paths.Bin},
		{"lib", paths.Lib},
		{"opt", paths.Opt},
		{"Taps", paths.Taps},
	}

	for _, d := range dirs {
		info, err := os.Stat(d.path)
		if err != nil {
			continue
		}
		perm := info.Mode().Perm()
		if perm&0002 != 0 {
			findings = append(findings, vulnFinding{
				Package:  "(global)",
				Severity: severityCritical,
				Category: "permissions",
				Detail:   fmt.Sprintf("directory %s (%s) is world-writable (%04o)", d.label, d.path, perm),
			})
		}
	}

	// Check if prefix is under $HOME.
	home, err := os.UserHomeDir()
	if err == nil && strings.HasPrefix(paths.Root, home+string(filepath.Separator)) {
		findings = append(findings, vulnFinding{
			Package:  "(global)",
			Severity: severityLow,
			Category: "isolation",
			Detail: fmt.Sprintf("grew prefix %s is under $HOME — sandboxed builds can potentially access sensitive files; "+
				"consider running 'sudo grew setup' for %s", paths.Root, config.SystemPrefix()),
		})
	}

	return findings
}

func printVulnJSON(findings []vulnFinding) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(map[string]any{
		"findings": findings,
		"total":    len(findings),
		"summary":  vulnSummary(findings),
	})
}

func printVulnText(findings []vulnFinding) error {
	if len(findings) == 0 {
		fmt.Println("No vulnerabilities found.")
		return nil
	}

	// Group by severity for display.
	bySeverity := map[vulnSeverity][]vulnFinding{
		severityCritical: {},
		severityHigh:     {},
		severityMedium:   {},
		severityLow:      {},
	}
	for _, f := range findings {
		bySeverity[f.Severity] = append(bySeverity[f.Severity], f)
	}

	for _, sev := range []vulnSeverity{severityCritical, severityHigh, severityMedium, severityLow} {
		group := bySeverity[sev]
		if len(group) == 0 {
			continue
		}
		fmt.Printf("\n[%s] %d finding(s):\n", strings.ToUpper(string(sev)), len(group))
		for _, f := range group {
			pkg := f.Package
			if f.Version != "" {
				pkg += " " + f.Version
			}
			fmt.Printf("  %s (%s): %s\n", pkg, f.Category, f.Detail)
		}
	}

	summary := vulnSummary(findings)
	fmt.Printf("\nScan complete: %d critical, %d high, %d medium, %d low\n",
		summary["critical"], summary["high"], summary["medium"], summary["low"])

	// Non-zero exit if critical or high findings exist.
	if summary["critical"] > 0 || summary["high"] > 0 {
		return fmt.Errorf("vulnerability scan found %d critical and %d high severity issue(s)",
			summary["critical"], summary["high"])
	}
	return nil
}

func vulnSummary(findings []vulnFinding) map[string]int {
	summary := map[string]int{
		"critical": 0,
		"high":     0,
		"medium":   0,
		"low":      0,
	}
	for _, f := range findings {
		summary[string(f.Severity)]++
	}
	return summary
}
