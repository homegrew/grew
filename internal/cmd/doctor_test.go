package cmd

import (
	"fmt"
	"strings"
	"testing"

	"github.com/homegrew/grew/internal/cask"
	"github.com/homegrew/grew/internal/formula"
)

func assertDoctorWarnings(t *testing.T, ctx *doctorCtx, checkFn func(*doctorCtx), wantWarn string) {
	t.Helper()
	var warnings []string
	ctx.warn = func(format string, args ...any) {
		warnings = append(warnings, fmt.Sprintf(format, args...))
	}

	checkFn(ctx)

	if wantWarn == "" {
		if len(warnings) > 0 {
			t.Errorf("expected no warnings, got: %v", warnings)
		}
	} else {
		found := false
		for _, w := range warnings {
			if strings.Contains(w, wantWarn) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected warning containing %q, got: %v", wantWarn, warnings)
		}
	}
}

func TestCheckCaskSHA256(t *testing.T) {
	tests := []struct {
		name     string
		casks    []*cask.Cask
		wantWarn string
	}{
		{
			name: "valid sha256",
			casks: []*cask.Cask{
				{
					Name:   "foo",
					SHA256: map[string]string{"darwin_arm64": strings.Repeat("a", 64)},
				},
			},
			wantWarn: "",
		},
		{
			name: "invalid sha256 length",
			casks: []*cask.Cask{
				{
					Name:   "foo",
					SHA256: map[string]string{"darwin_arm64": "abc"},
				},
			},
			wantWarn: "cask foo: SHA256 for darwin_arm64 has wrong length (3, expected 64)",
		},
		{
			name: "invalid sha256 characters",
			casks: []*cask.Cask{
				{
					Name:   "foo",
					SHA256: map[string]string{"darwin_arm64": strings.Repeat("z", 64)},
				},
			},
			wantWarn: "cask foo: SHA256 for darwin_arm64 contains non-hex character \"z\"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &doctorCtx{casks: tt.casks}
			assertDoctorWarnings(t, ctx, checkCaskSHA256, tt.wantWarn)
		})
	}
}

func TestCheckCaskSHA512(t *testing.T) {
	tests := []struct {
		name     string
		casks    []*cask.Cask
		wantWarn string
	}{
		{
			name: "valid sha512",
			casks: []*cask.Cask{
				{
					Name:   "foo",
					SHA512: map[string]string{"darwin_arm64": strings.Repeat("a", 128)},
				},
			},
			wantWarn: "",
		},
		{
			name: "invalid sha512 length",
			casks: []*cask.Cask{
				{
					Name:   "foo",
					SHA512: map[string]string{"darwin_arm64": "abc"},
				},
			},
			wantWarn: "cask foo: SHA512 for darwin_arm64 has wrong length (3, expected 128)",
		},
		{
			name: "invalid sha512 characters",
			casks: []*cask.Cask{
				{
					Name:   "foo",
					SHA512: map[string]string{"darwin_arm64": strings.Repeat("z", 128)},
				},
			},
			wantWarn: "cask foo: SHA512 for darwin_arm64 contains non-hex character \"z\"",
		},
		{
			name: "missing sha512 when sha256 is present",
			casks: []*cask.Cask{
				{
					Name:   "foo",
					SHA256: map[string]string{"darwin_arm64": strings.Repeat("a", 64)},
				},
			},
			wantWarn: "cask foo: missing SHA512 metadata",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &doctorCtx{casks: tt.casks}
			assertDoctorWarnings(t, ctx, checkCaskSHA512, tt.wantWarn)
		})
	}
}

func TestCheckFormulaSHA512(t *testing.T) {
	tests := []struct {
		name     string
		formulas []*formula.Formula
		wantWarn string
	}{
		{
			name: "valid sha512",
			formulas: []*formula.Formula{
				{
					Name:   "foo",
					SHA512: map[string]string{"darwin_arm64": strings.Repeat("a", 128)},
				},
			},
			wantWarn: "",
		},
		{
			name: "invalid sha512 length",
			formulas: []*formula.Formula{
				{
					Name:   "foo",
					SHA512: map[string]string{"darwin_arm64": "abc"},
				},
			},
			wantWarn: "SHA512 for darwin_arm64 has wrong length (3, expected 128)",
		},
		{
			name: "invalid sha512 characters",
			formulas: []*formula.Formula{
				{
					Name:   "foo",
					SHA512: map[string]string{"darwin_arm64": strings.Repeat("z", 128)},
				},
			},
			wantWarn: "SHA512 for darwin_arm64 contains non-hex character \"z\"",
		},
		{
			name: "missing sha512 when sha256 is present",
			formulas: []*formula.Formula{
				{
					Name:   "foo",
					SHA256: map[string]string{"darwin_arm64": strings.Repeat("a", 64)},
				},
			},
			wantWarn: "formula foo: missing SHA512 metadata",
		},
		{
			name: "valid bottle sha512",
			formulas: []*formula.Formula{
				{
					Name: "foo",
					Bottle: map[string]formula.BottleSpec{
						"darwin_arm64": {
							SHA256: strings.Repeat("a", 64),
							SHA512: strings.Repeat("a", 128),
						},
					},
				},
			},
			wantWarn: "",
		},
		{
			name: "missing bottle sha512",
			formulas: []*formula.Formula{
				{
					Name: "foo",
					Bottle: map[string]formula.BottleSpec{
						"darwin_arm64": {
							SHA256: strings.Repeat("a", 64),
						},
					},
				},
			},
			wantWarn: "formula foo: bottle for darwin_arm64 missing SHA512",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &doctorCtx{formulas: tt.formulas}
			assertDoctorWarnings(t, ctx, checkFormulaSHA512, tt.wantWarn)
		})
	}
}
