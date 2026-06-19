package main

import "github.com/homegrew/grew/pkg/formula"

// formulaBuildOverrides supplies source-build configuration that cannot be
// derived from the Homebrew JSON API. Homebrew formulas express build logic as
// arbitrary Ruby (`def install`), so details like "the configure script lives
// in the unix/ subdirectory" are invisible to the JSON conversion. This table
// reinstates them for the handful of core formulas grew supports building from
// source. Keyed by formula name.
//
// To add a formula: set WorkingDir for a configure-in-subdirectory project,
// and/or Configure/Install to override the default ./configure && make &&
// make install commands (use the "{prefix}" placeholder for the keg path).
var formulaBuildOverrides = map[string]formula.BuildSpec{
	// Tcl/Tk keep their Unix configure script under unix/; Homebrew does
	// `cd "unix"` before configuring. Applies to every Tcl-versioned variant.
	"tcl-tk":   {WorkingDir: "unix"},
	"tcl-tk@8": {WorkingDir: "unix"},
	// ncurses requires specific configure flags to build shared libraries (.dylib)
	// and pkg-config files, which aren't enabled by the default source-build flow.
	"ncurses": {
		Configure: []string{
			"./configure",
			"--prefix={prefix}",
			"--enable-pc-files",
			"--with-shared",
			"--with-cxx-shared",
			"--enable-widec",
		},
	},
}

// applyFormulaOverrides merges any known build override into f. Fields already
// populated on the formula win, so a future conversion that learns to derive
// build config from upstream metadata is never clobbered by this table.
func applyFormulaOverrides(f *formula.Formula) {
	ov, ok := formulaBuildOverrides[f.Name]
	if !ok {
		return
	}
	if f.Build.WorkingDir == "" {
		f.Build.WorkingDir = ov.WorkingDir
	}
	if len(f.Build.Configure) == 0 {
		f.Build.Configure = ov.Configure
	}
	if len(f.Build.Install) == 0 {
		f.Build.Install = ov.Install
	}
}
