package cmd

import (
	"flag"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/homegrew/grew/internal/cask"
	"github.com/homegrew/grew/internal/config"
	"github.com/homegrew/grew/internal/depgraph"
	"github.com/homegrew/grew/internal/formula"
	"github.com/homegrew/grew/internal/snapshot"
	"github.com/homegrew/grew/internal/tap"
	"github.com/homegrew/grew/internal/validation"
)

// auditResult collects warnings and errors for a single formula or cask.
type auditResult struct {
	Name     string
	Warnings []string
	Errors   []string
}

func (r *auditResult) warnf(format string, args ...any) {
	r.Warnings = append(r.Warnings, fmt.Sprintf(format, args...))
}

func (r *auditResult) errorf(format string, args ...any) {
	r.Errors = append(r.Errors, fmt.Sprintf(format, args...))
}

func (r *auditResult) ok() bool {
	return len(r.Warnings) == 0 && len(r.Errors) == 0
}

func runAudit(args []string) error {
	fs := flag.NewFlagSet("audit", flag.ContinueOnError)
	strict := fs.Bool("strict", false, "Treat warnings as errors")
	isCask := fs.Bool("cask", false, "Audit casks instead of formulas")
	online := fs.Bool("online", false, "Include checks that require installed packages (snapshot verification)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	targets := fs.Args()

	paths := config.Default()
	tapMgr := &tap.Manager{TapsDir: paths.Taps}
	if err := tapMgr.InitCore(); err != nil {
		Debugf("init core tap: %v\n", err)
	}

	if *isCask {
		return runAuditCasks(paths, targets, *strict)
	}
	return runAuditFormulas(paths, targets, *strict, *online)
}

func runAuditFormulas(paths config.Paths, targets []string, strict, online bool) error {
	loader := newLoader(paths.Taps)

	var formulas []*formula.Formula
	if len(targets) == 0 {
		var err error
		formulas, err = loader.LoadAll()
		if err != nil {
			return fmt.Errorf("load formulas: %w", err)
		}
	} else {
		for _, name := range targets {
			f, err := loader.LoadByName(name)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: cannot load formula %s: %v\n", name, err)
				continue
			}
			formulas = append(formulas, f)
		}
	}

	if len(formulas) == 0 {
		fmt.Println("No formulas to audit.")
		return nil
	}

	// Build a name set for dependency resolution checks.
	allNames := make(map[string]bool, len(formulas))
	for _, f := range formulas {
		allNames[f.Name] = true
	}

	var totalWarnings, totalErrors int
	for _, f := range formulas {
		r := auditFormula(f, allNames, loader, paths, online)
		totalWarnings += len(r.Warnings)
		totalErrors += len(r.Errors)
		printAuditResult(r)
	}

	return auditSummary(totalWarnings, totalErrors, strict)
}

func auditFormula(f *formula.Formula, allNames map[string]bool, loader *formula.Loader, paths config.Paths, online bool) *auditResult {
	r := &auditResult{Name: f.Name}

	// Metadata completeness.
	if f.Description == "" {
		r.warnf("missing description")
	}
	if f.Homepage == "" {
		r.warnf("missing homepage")
	} else {
		if !strings.HasPrefix(f.Homepage, "https://") {
			r.warnf("homepage should use HTTPS: %s", f.Homepage)
		}
		if _, err := url.Parse(f.Homepage); err != nil {
			r.errorf("homepage is not a valid URL: %s", f.Homepage)
		}
	}
	if f.License == "" {
		r.warnf("missing license")
	}

	// Name conventions.
	if f.Name != strings.ToLower(f.Name) {
		r.warnf("name should be lowercase: %q", f.Name)
	}
	if !validation.IsValidName(f.Name) {
		r.errorf("name contains invalid characters: %q", f.Name)
	}
	if !validation.IsValidVersion(f.Version) {
		r.errorf("version contains invalid characters: %q", f.Version)
	}

	// URL checks — every platform URL must be HTTPS and parseable.
	for platform, u := range f.URL {
		auditURL(r, "url", platform, u)
	}
	for platform, b := range f.Bottle {
		auditURL(r, "bottle", platform, b.URL)
	}
	if f.Source.URL != "" {
		auditURL(r, "source", "", f.Source.URL)
	}
	if f.SourceURL != "" {
		auditURL(r, "source_url", "", f.SourceURL)
	}

	// SHA256 checks — every hash must be valid hex.
	for platform, hash := range f.SHA256 {
		if err := validation.ValidateSHA256(hash); err != nil {
			r.errorf("sha256 for %s: %v", platform, err)
		}
	}
	for platform, b := range f.Bottle {
		if err := validation.ValidateSHA256(b.SHA256); err != nil {
			r.errorf("bottle sha256 for %s: %v", platform, err)
		}
	}
	if f.Source.SHA256 != "" {
		if err := validation.ValidateSHA256(f.Source.SHA256); err != nil {
			r.errorf("source sha256: %v", err)
		}
	}
	if f.SourceSHA256 != "" {
		if err := validation.ValidateSHA256(f.SourceSHA256); err != nil {
			r.errorf("source_sha256: %v", err)
		}
	}

	// No download URLs at all.
	if len(f.URL) == 0 && len(f.Bottle) == 0 && f.Source.URL == "" && f.SourceURL == "" {
		r.errorf("no download URLs defined")
	}

	// Dependency checks.
	for _, dep := range f.Dependencies {
		if !validation.IsValidName(dep) {
			r.errorf("dependency %q has invalid name", dep)
		} else if !allNames[dep] {
			r.warnf("dependency %q not found in tap", dep)
		}
	}
	for _, dep := range f.BuildDependencies {
		if !validation.IsValidName(dep) {
			r.errorf("build_dependency %q has invalid name", dep)
		}
	}

	// Circular dependency check.
	resolver := &depgraph.Resolver{Loader: loader}
	if _, err := resolver.Resolve(f.Name); err != nil {
		if strings.Contains(err.Error(), "cycle") {
			r.errorf("circular dependency: %v", err)
		} else {
			// Missing dep or load error — already warned above likely.
			Debugf("resolve %s: %v\n", f.Name, err)
		}
	}

	// Install spec.
	if f.Install.Type != "" && f.Install.Type != "binary" && f.Install.Type != "archive" {
		r.errorf("install.type must be \"binary\" or \"archive\", got %q", f.Install.Type)
	}
	if f.Install.Type == "binary" && f.Install.BinaryName == "" {
		r.warnf("install.type is binary but binary_name is not set")
	}

	// Self-dependency.
	for _, dep := range f.Dependencies {
		if dep == f.Name {
			r.errorf("formula depends on itself")
			break
		}
	}

	// Online checks (installed package verification).
	if online {
		auditFormulaInstalled(r, f, paths)
	}

	return r
}

func auditFormulaInstalled(r *auditResult, f *formula.Formula, paths config.Paths) {
	kegPath := fmt.Sprintf("%s/%s/%s", paths.Cellar, f.Name, f.Version)
	if !snapshot.Exists(kegPath) {
		return
	}
	result, err := snapshot.Verify(kegPath)
	if err != nil {
		r.warnf("snapshot verify: %v", err)
		return
	}
	if result.OK {
		return
	}
	for _, mf := range result.Missing {
		r.errorf("installed file missing: %s", mf)
	}
	for _, mf := range result.Modified {
		r.errorf("installed file modified: %s", mf)
	}
	for _, mf := range result.Added {
		r.warnf("unexpected file in keg: %s", mf)
	}
}

func runAuditCasks(paths config.Paths, targets []string, strict bool) error {
	caskLoader := &cask.Loader{TapDir: paths.Taps}
	if Debug {
		caskLoader.DebugLog = func(format string, args ...any) {
			Debugf(format, args...)
		}
	}

	var casks []*cask.Cask
	if len(targets) == 0 {
		var err error
		casks, err = caskLoader.LoadAll()
		if err != nil {
			return fmt.Errorf("load casks: %w", err)
		}
	} else {
		for _, name := range targets {
			c, err := caskLoader.LoadByName(name)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: cannot load cask %s: %v\n", name, err)
				continue
			}
			casks = append(casks, c)
		}
	}

	if len(casks) == 0 {
		fmt.Println("No casks to audit.")
		return nil
	}

	var totalWarnings, totalErrors int
	for _, c := range casks {
		r := auditCask(c)
		totalWarnings += len(r.Warnings)
		totalErrors += len(r.Errors)
		printAuditResult(r)
	}

	return auditSummary(totalWarnings, totalErrors, strict)
}

func auditCask(c *cask.Cask) *auditResult {
	r := &auditResult{Name: c.Name}

	// Metadata completeness.
	if c.Description == "" {
		r.warnf("missing description")
	}
	if c.Homepage == "" {
		r.warnf("missing homepage")
	} else {
		if !strings.HasPrefix(c.Homepage, "https://") {
			r.warnf("homepage should use HTTPS: %s", c.Homepage)
		}
		if _, err := url.Parse(c.Homepage); err != nil {
			r.errorf("homepage is not a valid URL: %s", c.Homepage)
		}
	}
	if c.License == "" {
		r.warnf("missing license")
	}

	// Name conventions.
	if c.Name != strings.ToLower(c.Name) {
		r.warnf("name should be lowercase: %q", c.Name)
	}
	if !validation.IsValidName(c.Name) {
		r.errorf("name contains invalid characters: %q", c.Name)
	}
	if !validation.IsValidVersion(c.Version) {
		r.errorf("version contains invalid characters: %q", c.Version)
	}

	// URL checks.
	for platform, u := range c.URL {
		auditURL(r, "url", platform, u)
	}

	// SHA256 checks.
	for platform, hash := range c.SHA256 {
		if err := validation.ValidateSHA256(hash); err != nil {
			r.errorf("sha256 for %s: %v", platform, err)
		}
	}

	// Artifact checks.
	if len(c.Artifacts.App) == 0 && len(c.Artifacts.Pkg) == 0 && len(c.Artifacts.Bin) == 0 {
		r.errorf("no artifacts defined (need at least one of: app, pkg, bin)")
	}
	for _, app := range c.Artifacts.App {
		if !strings.HasSuffix(app, ".app") {
			r.errorf("app artifact %q must end with .app", app)
		}
	}

	return r
}

// auditURL validates a single URL field.
func auditURL(r *auditResult, field, platform, rawURL string) {
	label := field
	if platform != "" {
		label = fmt.Sprintf("%s[%s]", field, platform)
	}
	if rawURL == "" {
		r.errorf("%s: empty URL", label)
		return
	}
	if !strings.HasPrefix(rawURL, "https://") {
		r.errorf("%s: must use HTTPS: %s", label, rawURL)
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		r.errorf("%s: invalid URL: %v", label, err)
		return
	}
	if u.Host == "" {
		r.errorf("%s: URL has no host: %s", label, rawURL)
	}
}

func printAuditResult(r *auditResult) {
	if r.ok() {
		return
	}
	for _, e := range r.Errors {
		fmt.Printf("%s: error: %s\n", r.Name, e)
	}
	for _, w := range r.Warnings {
		fmt.Printf("%s: warning: %s\n", r.Name, w)
	}
}

func auditSummary(warnings, errors int, strict bool) error {
	total := warnings + errors
	if total == 0 {
		fmt.Println("All definitions passed audit.")
		return nil
	}
	fmt.Printf("\n%d error(s) and %d warning(s) found.\n", errors, warnings)
	if errors > 0 || (strict && warnings > 0) {
		return fmt.Errorf("audit failed")
	}
	return nil
}
