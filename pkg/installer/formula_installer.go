package installer

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/homegrew/grew/pkg/auditlog"
	"github.com/homegrew/grew/pkg/caveats"
	grewctx "github.com/homegrew/grew/pkg/context"
	"github.com/homegrew/grew/pkg/downloader"
	"github.com/homegrew/grew/pkg/formula"
	"github.com/homegrew/grew/pkg/fsutil"
	"github.com/homegrew/grew/pkg/hooks"
	"github.com/homegrew/grew/pkg/logger"
	"github.com/homegrew/grew/pkg/receipt"
	"github.com/homegrew/grew/pkg/relocation"
	"github.com/homegrew/grew/pkg/safepath"
	"github.com/homegrew/grew/pkg/sandbox"
	"github.com/homegrew/grew/pkg/signing"
	"github.com/homegrew/grew/pkg/snapshot"
	"github.com/homegrew/grew/pkg/ui"
)

type InstallOpts struct {
	SkipPostInstall    bool
	SkipLink           bool
	InstalledOnRequest bool
	// ForceBottle pours a bottle even when one for the exact current macOS
	// version is not available, using the newest available macOS version's
	// bottle instead (mirrors `brew install --force-bottle`).
	ForceBottle bool
	// HookSet carries lifecycle hooks executed at build and install phases.
	// A nil HookSet is a no-op.
	HookSet *hooks.HookSet
	// CaveatRenderer renders post-install caveats after hooks complete.
	// A nil renderer skips caveat output.
	CaveatRenderer *caveats.Renderer
}

func InstallFormula(f *formula.Formula, ctx *grewctx.InstallContext, opts InstallOpts) (err error) {
	paths := ctx.Paths
	defer logger.TimeOp(fmt.Sprintf("install %s %s", f.Name, f.Version))()
	slog.Debug(fmt.Sprintf("platform: %s, install type: %s, keg_only: %v", formula.PlatformKey(), f.Install.Type, f.KegOnly))

	defer func() {
		if err != nil {
			slog.Error("installation failed, cleaning up", "formula", f.Name, "error", err)
			_ = ctx.Linker.Unlink(f.Name)
			_ = ctx.Cellar.UninstallVersion(f.Name, f.Version)
		}
	}()

	ui.FprintArrow(os.Stderr, "Installing %s %s", f.Name, f.Version)

	var dlURL, sha256, sha512 string
	if opts.ForceBottle {
		dlURL, sha256, sha512, err = f.ResolveForceBottle()
		if err != nil {
			return err
		}
	} else {
		dlURL, err = f.GetURL()
		if err != nil {
			return err
		}
		sha256, err = f.GetSHA256()
		if err != nil {
			return err
		}
		sha512, err = f.GetSHA512()
		if err != nil {
			return err
		}
	}
	slog.Info("URL: " + dlURL)

	slog.Info("expected SHA256: " + sha256)
	if sha512 != "" {
		slog.Info("expected SHA512: " + sha512)
	}

	if err := safepath.SafePathComponent(f.Name); err != nil {
		return fmt.Errorf("invalid formula name: %w", err)
	}
	if err := safepath.SafePathComponent(f.Version); err != nil {
		return fmt.Errorf("invalid formula version: %w", err)
	}

	ext := safepath.URLExt(dlURL)
	if ext == "" && f.Install.Format != "" {
		ext = "." + f.Install.Format
	}
	filename := f.Name + "-" + f.Version + ext
	if err := safepath.SafePathComponent(filename); err != nil {
		return fmt.Errorf("invalid download filename: %w", err)
	}

	localFile, err := safepath.SafeJoin(paths.Tmp, filename)
	if err != nil {
		return fmt.Errorf("invalid download path: %w", err)
	}
	if _, err := os.Stat(localFile); err == nil {
		if err := downloader.VerifySHA256(localFile, sha256); err == nil {
			ui.FprintArrow(os.Stderr, "Using cached %s", filename)
		} else {
			localFile, err = ctx.DL.Download(dlURL, filename)
			if err != nil {
				return fmt.Errorf("download %s: %w", f.Name, err)
			}
		}
	} else {
		localFile, err = ctx.DL.Download(dlURL, filename)
		if err != nil {
			return fmt.Errorf("download %s: %w", f.Name, err)
		}
	}

	if err := VerifySignature(f.Name, sha256, f.GetSignature(), paths.Root); err != nil {
		_ = fsutil.RemoveIfWithinAllowed(paths.Tmp, paths.Cache, localFile)
		return err
	}

	stageDir, err := safepath.SafeJoin(paths.Tmp, f.Name+"-"+f.Version+"-stage")
	if err != nil {
		return fmt.Errorf("invalid stage directory: %w", err)
	}
	os.RemoveAll(stageDir)

	installSpec := f.Install
	if installSpec.Type == "" {
		installSpec.Type = "archive"
		installSpec.Format = "tar.gz"
		installSpec.StripComponents = 2
	}

	ui.FprintArrow(os.Stderr, "Extracting (sandboxed)")
	if err := SandboxedExtract(localFile, stageDir, installSpec); err != nil {
		os.RemoveAll(stageDir)
		os.Remove(localFile)
		return fmt.Errorf("extract %s: %w", f.Name, err)
	}

	kegPath, err := ctx.Cellar.KegPath(f.Name, f.Version)
	if err != nil {
		os.RemoveAll(stageDir)
		os.Remove(localFile)
		return fmt.Errorf("keg path %s: %w", f.Name, err)
	}
	if err := ctx.Cellar.Install(f.Name, f.Version, stageDir); err != nil {
		os.RemoveAll(stageDir)
		os.Remove(localFile)
		return fmt.Errorf("cellar install %s: %w", f.Name, err)
	}

	if relErr := relocation.RelocateKeg(kegPath, paths.Root); relErr != nil {
		return fmt.Errorf("relocate %s: %w", f.Name, relErr)
	}

	return FinalizeInstall(f, ctx, finalizeOpts{
		kegPath: kegPath,
		meta: snapshot.InstallMeta{
			Platform:           formula.PlatformKey(),
			DownloadURL:        dlURL,
			DownloadSHA256:     sha256,
			DownloadSHA512:     sha512,
			Dependencies:       f.Dependencies,
			InstalledOnRequest: opts.InstalledOnRequest,
			BuiltFromSource:    false,
		},
		skipLink:       opts.SkipLink,
		skipPostInst:   opts.SkipPostInstall,
		hookSet:        opts.HookSet,
		caveatRenderer: opts.CaveatRenderer,
		auditSHA256:    sha256,
		auditDetail:    "bottle",
		cleanup: func() {
			os.RemoveAll(stageDir)
		},
	})
}

func canonicalSubpath(base, target string) (string, error) {
	canonBase := filepath.Clean(base)
	if eval, err := filepath.EvalSymlinks(canonBase); err == nil {
		canonBase = filepath.Clean(eval)
	}
	if abs, err := filepath.Abs(canonBase); err == nil {
		canonBase = filepath.Clean(abs)
	}
	if err := safepath.SafeAbsolutePath(canonBase); err != nil {
		return "", fmt.Errorf("invalid base path %q: %w", canonBase, err)
	}

	canonTarget := filepath.Clean(target)
	if eval, err := filepath.EvalSymlinks(canonTarget); err == nil {
		canonTarget = filepath.Clean(eval)
	}
	if abs, err := filepath.Abs(canonTarget); err == nil {
		canonTarget = filepath.Clean(abs)
	}
	if err := safepath.SafeAbsolutePath(canonTarget); err != nil {
		return "", fmt.Errorf("invalid target path %q: %w", canonTarget, err)
	}
	if err := safepath.CheckSubpath(canonBase, canonTarget); err != nil {
		return "", err
	}
	return canonTarget, nil
}

func InstallFormulaFromSource(f *formula.Formula, ctx *grewctx.InstallContext, opts InstallOpts) (err error) {
	paths := ctx.Paths
	defer logger.TimeOp(fmt.Sprintf("build from source %s %s", f.Name, f.Version))()

	defer func() {
		if err != nil {
			slog.Error("installation from source failed, cleaning up", "formula", f.Name, "error", err)
			_ = ctx.Linker.Unlink(f.Name)
			_ = ctx.Cellar.UninstallVersion(f.Name, f.Version)
		}
	}()

	ui.FprintArrow(os.Stderr, "Building %s %s from source", f.Name, f.Version)

	srcURL, err := f.GetSourceURL()
	if err != nil {
		return err
	}
	srcSHA256, err := f.GetSourceSHA256()
	if err != nil {
		return err
	}
	srcSHA512, err := f.GetSourceSHA512()
	if err != nil {
		return err
	}

	if err := safepath.SafePathComponent(f.Name); err != nil {
		return fmt.Errorf("invalid formula name %q: %w", f.Name, err)
	}
	if err := safepath.SafePathComponent(f.Version); err != nil {
		return fmt.Errorf("invalid formula version %q: %w", f.Version, err)
	}

	ext := safepath.URLExt(srcURL)
	filename := f.Name + "-" + f.Version + "-src" + ext

	localFile, err := ctx.DL.Download(srcURL, filename)
	if err != nil {
		return fmt.Errorf("download source %s: %w", f.Name, err)
	}

	safeLocalFile, err := canonicalSubpath(paths.Tmp, localFile)
	if err != nil {
		return fmt.Errorf("invalid downloaded file path %q: %w", localFile, err)
	}
	safeLocalName := filepath.Base(safeLocalFile)
	if err := safepath.SafePathComponent(safeLocalName); err != nil {
		return fmt.Errorf("invalid downloaded file name %q: %w", safeLocalName, err)
	}
	safeLocalFile, err = safepath.SafeJoin(paths.Tmp, safeLocalName)
	if err != nil {
		return fmt.Errorf("invalid downloaded file path %q: %w", localFile, err)
	}

	if err := VerifySignature(f.Name, srcSHA256, f.GetSourceSignature(), paths.Root); err != nil {
		if subErr := safepath.CheckSubpath(paths.Tmp, safeLocalFile); subErr != nil {
			return fmt.Errorf("refusing to remove file outside temp directory %q: %w", safeLocalFile, subErr)
		}
		os.Remove(safeLocalFile)
		return err
	}

	buildDir, err := safepath.SafeJoin(paths.Tmp, f.Name+"-"+f.Version+"-build")
	if err != nil {
		return fmt.Errorf("invalid build directory: %w", err)
	}
	os.RemoveAll(buildDir)
	srcSpec := formula.InstallSpec{Type: "archive", StripComponents: 1, Format: f.Install.Format}
	if err := SandboxedExtract(safeLocalFile, buildDir, srcSpec); err != nil {
		os.RemoveAll(buildDir)
		os.Remove(safeLocalFile)
		return fmt.Errorf("extract source %s: %w", f.Name, err)
	}

	kegPath, err := ctx.Cellar.KegPath(f.Name, f.Version)
	if err != nil {
		os.RemoveAll(buildDir)
		os.Remove(safeLocalFile)
		return fmt.Errorf("keg path %s: %w", f.Name, err)
	}
	if err := os.MkdirAll(kegPath, 0755); err != nil {
		os.RemoveAll(buildDir)
		os.Remove(safeLocalFile)
		return fmt.Errorf("create keg dir: %w", err)
	}

	var depPaths []string
	for _, dep := range f.Dependencies {
		if err := safepath.SafePathComponent(dep); err != nil {
			os.RemoveAll(buildDir)
			os.Remove(safeLocalFile)
			return fmt.Errorf("invalid dependency path component %q: %w", dep, err)
		}
		depCellar, err := safepath.SafeJoin(paths.Cellar, dep)
		if err != nil {
			os.RemoveAll(buildDir)
			os.Remove(safeLocalFile)
			return fmt.Errorf("invalid dependency cellar path %q: %w", dep, err)
		}
		depOpt, err := safepath.SafeJoin(paths.Opt, dep)
		if err != nil {
			os.RemoveAll(buildDir)
			os.Remove(safeLocalFile)
			return fmt.Errorf("invalid dependency opt path %q: %w", dep, err)
		}
		depPaths = append(depPaths, depCellar, depOpt)
	}

	sbCfg := sandbox.BuildConfig{
		BuildDir: buildDir,
		KegDir:   kegPath,
		DepPaths: depPaths,
	}

	cleanup := func() {
		os.RemoveAll(buildDir)
	}

	// Build commands run in workDir, which defaults to the extracted source
	// root but can be a subdirectory (f.Build.WorkingDir) for projects like
	// tcl-tk whose configure script lives under "unix/".
	workDir := buildDir
	if f.Build.WorkingDir != "" {
		workDir, err = safepath.SafeJoin(buildDir, f.Build.WorkingDir)
		if err != nil {
			os.RemoveAll(kegPath)
			cleanup()
			return fmt.Errorf("invalid build working_dir %q for %s: %w", f.Build.WorkingDir, f.Name, err)
		}
		if info, statErr := os.Stat(workDir); statErr != nil || !info.IsDir() {
			os.RemoveAll(kegPath)
			cleanup()
			return fmt.Errorf("build working_dir %q not found in source tree for %s", f.Build.WorkingDir, f.Name)
		}
	}

	// configure step: honor f.Build.Configure when set (with {prefix}
	// expansion), otherwise the conventional ./configure --prefix=<keg>.
	configureArgs := []string{"./configure", "--prefix=" + kegPath}
	if len(f.Build.Configure) > 0 {
		configureArgs = expandBuildVars(f.Build.Configure, kegPath)
	}
	configure := sandbox.Command(sbCfg, configureArgs[0], configureArgs[1:]...)
	configure.Dir = workDir
	configure.Stdout = os.Stdout
	configure.Stderr = os.Stderr
	if err := configure.Run(); err != nil {
		os.RemoveAll(kegPath)
		cleanup()
		return fmt.Errorf("configure %s: %w", f.Name, err)
	}

	makeCmd := sandbox.Command(sbCfg, "make")
	makeCmd.Dir = workDir
	makeCmd.Stdout = os.Stdout
	makeCmd.Stderr = os.Stderr
	if err := makeCmd.Run(); err != nil {
		os.RemoveAll(kegPath)
		cleanup()
		return fmt.Errorf("make %s: %w", f.Name, err)
	}

	// install step: honor f.Build.Install when set, otherwise `make install`.
	installArgs := []string{"make", "install"}
	if len(f.Build.Install) > 0 {
		installArgs = expandBuildVars(f.Build.Install, kegPath)
	}
	makeInstall := sandbox.Command(sbCfg, installArgs[0], installArgs[1:]...)
	makeInstall.Dir = workDir
	makeInstall.Stdout = os.Stdout
	makeInstall.Stderr = os.Stderr
	if err := makeInstall.Run(); err != nil {
		os.RemoveAll(kegPath)
		cleanup()
		return fmt.Errorf("make install %s: %w", f.Name, err)
	}

	if opts.HookSet != nil {
		hookEnv := hooks.Env{
			Prefix:  paths.Root,
			Cellar:  paths.Cellar,
			Formula: f.Name,
			Version: f.Version,
			Tmpdir:  buildDir,
		}
		if err := opts.HookSet.RunPhase(context.Background(), hooks.PhasePostBuild, hookEnv); err != nil {
			os.RemoveAll(kegPath)
			cleanup()
			return fmt.Errorf("post-build hook for %s: %w", f.Name, err)
		}
	}

	return FinalizeInstall(f, ctx, finalizeOpts{
		kegPath: kegPath,
		meta: snapshot.InstallMeta{
			Platform:           formula.PlatformKey(),
			DownloadURL:        srcURL,
			DownloadSHA256:     srcSHA256,
			DownloadSHA512:     srcSHA512,
			Dependencies:       f.Dependencies,
			InstalledOnRequest: opts.InstalledOnRequest,
			BuiltFromSource:    true,
		},
		skipLink:       opts.SkipLink,
		skipPostInst:   opts.SkipPostInstall,
		hookSet:        opts.HookSet,
		caveatRenderer: opts.CaveatRenderer,
		auditSHA256:    srcSHA256,
		auditDetail:    "source",
		cleanup:        cleanup,
	})
}

// expandBuildVars substitutes build-time placeholders in custom build command
// arguments. Currently only "{prefix}" (the keg install prefix) is supported,
// letting static formula definitions reference the dynamic keg path in custom
// configure/install commands.
func expandBuildVars(args []string, prefix string) []string {
	out := make([]string, len(args))
	for i, a := range args {
		out[i] = strings.ReplaceAll(a, "{prefix}", prefix)
	}
	return out
}

func VerifySignature(name, sha256Hex, signatureB64, grewRoot string) error {
	trustedKeys, err := signing.LoadTrustedKeys(grewRoot)
	if err != nil {
		return fmt.Errorf("load trusted keys: %w", err)
	}
	if len(trustedKeys) == 0 {
		return nil
	}
	if signatureB64 == "" {
		slog.Warn(fmt.Sprintf("%s has no signature (trusted keys are configured)", name))
		return nil
	}
	if !signing.VerifyAny(trustedKeys, sha256Hex, signatureB64) {
		return fmt.Errorf("signature verification failed for %s: not signed by any trusted key", name)
	}
	ui.FprintArrow(os.Stderr, "Signature verified")
	return nil
}

type finalizeOpts struct {
	kegPath        string
	meta           snapshot.InstallMeta
	skipLink       bool
	skipPostInst   bool
	hookSet        *hooks.HookSet
	caveatRenderer *caveats.Renderer
	auditSHA256    string
	auditDetail    string
	cleanup        func()
}

func FinalizeInstall(f *formula.Formula, ctx *grewctx.InstallContext, opts finalizeOpts) error {
	if !opts.skipLink {
		if err := ctx.Linker.Link(f.Name, f.Version, f.EffectiveKegOnly()); err != nil {
			return fmt.Errorf("link %s: %w", f.Name, err)
		}
	} else {
		// Ensure any existing links are removed if skipLink is requested.
		_ = ctx.Linker.Unlink(f.Name)
	}

	if !opts.meta.BuiltFromSource {
		if issues := relocation.VerifyKeg(opts.kegPath, ctx.Paths.Root); len(issues) > 0 {
			return fmt.Errorf("linkage verification failed for %s", f.Name)
		}
	}

	manifest, _ := snapshot.Capture(f.Name, f.Version, opts.kegPath, opts.meta)
	_ = snapshot.Save(manifest, opts.kegPath)

	r := &receipt.Receipt{
		Name:                f.Name,
		Version:             f.Version,
		BuiltFromSource:     opts.meta.BuiltFromSource,
		PouredFromBottle:    !opts.meta.BuiltFromSource,
		InstalledAt:         time.Now(),
		Dependencies:        f.Dependencies,
		RuntimeDependencies: f.Dependencies,
		InstalledOnRequest:  opts.meta.InstalledOnRequest,
	}
	_ = receipt.Save(r, opts.kegPath)

	if opts.cleanup != nil {
		opts.cleanup()
	}

	if err := RunPostInstall(f, opts.kegPath, opts.skipPostInst); err != nil {
		return err
	}

	if opts.hookSet != nil {
		piTmp, _ := os.MkdirTemp("", "grew-hook-postinstall-*")
		defer os.RemoveAll(piTmp)
		hookEnv := hooks.Env{
			Prefix:  ctx.Paths.Root,
			Cellar:  ctx.Paths.Cellar,
			Formula: f.Name,
			Version: f.Version,
			Tmpdir:  piTmp,
		}
		if err := opts.hookSet.RunPhase(context.Background(), hooks.PhasePostInstall, hookEnv); err != nil {
			return fmt.Errorf("post-install hook for %s: %w", f.Name, err)
		}
	}

	if opts.caveatRenderer != nil {
		if err := opts.caveatRenderer.Render(*f, ctx.Paths.Root); err != nil {
			return fmt.Errorf("caveats for %s: %w", f.Name, err)
		}
	}

	if ctx.AuditLog != nil {
		ctx.AuditLog.Log(auditlog.ActionInstall, f.Name, f.Version, opts.auditSHA256, opts.auditDetail)
	}

	ui.FprintArrow(os.Stderr, "%s %s installed", f.Name, f.Version)
	return nil
}

func RunPostInstall(f *formula.Formula, kegPath string, skip bool) error {
	if f.PostInstall == "" || skip {
		return nil
	}
	piTmp, _ := os.MkdirTemp("", "grew-postinstall-*")
	defer os.RemoveAll(piTmp)
	piCfg := sandbox.PostInstallConfig{KegDir: kegPath, TmpDir: piTmp}
	cmd := sandbox.PostInstallCommand(piCfg, "sh", "-c", f.PostInstall)
	cmd.Dir = kegPath
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
