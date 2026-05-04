package bpatch

import (
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/homegrew/grew/internal/downloader"
	"github.com/homegrew/grew/internal/release"
)

func TestVerifyPatchChecksum(t *testing.T) {
	t.Setenv("HOMEGREW_ALLOWED_HOSTS", "example.com")
	patchContent := []byte("fake-patch-data")

	h256 := sha256.Sum256(patchContent)
	expectedSHA256 := hex.EncodeToString(h256[:])

	h512 := sha512.Sum512(patchContent)
	expectedSHA512 := hex.EncodeToString(h512[:])

	tmpDir := t.TempDir()
	patchFile := filepath.Join(tmpDir, "test.patch")
	if err := os.WriteFile(patchFile, patchContent, 0644); err != nil {
		t.Fatalf("failed to write patch file: %v", err)
	}

	tests := []struct {
		name         string
		serveMux     func(mux *http.ServeMux)
		assets       func(serverURL string) []release.Asset
		patchName    string
		expectErr    bool
		errSubstring string
	}{
		{
			name: "standalone sha256 success",
			serveMux: func(mux *http.ServeMux) {
				mux.HandleFunc("/test.patch.sha256", func(w http.ResponseWriter, r *http.Request) {
					w.Write([]byte(expectedSHA256 + "  test.patch"))
				})
			},
			assets: func(serverURL string) []release.Asset {
				return []release.Asset{
					{Name: "test.patch.sha256", BrowserDownloadURL: serverURL + "/test.patch.sha256"},
				}
			},
			patchName: "test.patch",
			expectErr: false,
		},
		{
			name: "standalone sha256 mismatch",
			serveMux: func(mux *http.ServeMux) {
				mux.HandleFunc("/test.patch.sha256", func(w http.ResponseWriter, r *http.Request) {
					w.Write([]byte(strings.Repeat("0", 64) + "  test.patch"))
				})
			},
			assets: func(serverURL string) []release.Asset {
				return []release.Asset{
					{Name: "test.patch.sha256", BrowserDownloadURL: serverURL + "/test.patch.sha256"},
				}
			},
			patchName:    "test.patch",
			expectErr:    true,
			errSubstring: "SHA-256 mismatch",
		},
		{
			name: "fallback monolithic checksums.txt success",
			serveMux: func(mux *http.ServeMux) {
				mux.HandleFunc("/checksums.txt", func(w http.ResponseWriter, r *http.Request) {
					w.Write([]byte(expectedSHA256 + "  test.patch\n"))
				})
			},
			assets: func(serverURL string) []release.Asset {
				return []release.Asset{
					{Name: "checksums.txt", BrowserDownloadURL: serverURL + "/checksums.txt"},
				}
			},
			patchName: "test.patch",
			expectErr: false,
		},
		{
			name: "fallback monolithic checksums.txt success",
			serveMux: func(mux *http.ServeMux) {
				mux.HandleFunc("/checksums.txt", func(w http.ResponseWriter, r *http.Request) {
					w.Write([]byte(expectedSHA512 + "  test.patch\n"))
				})
			},
			assets: func(serverURL string) []release.Asset {
				return []release.Asset{
					{Name: "checksums.txt", BrowserDownloadURL: serverURL + "/checksums.txt"},
				}
			},
			patchName: "test.patch",
			expectErr: false,
		},
		{
			name: "fallback monolithic mismatch",
			serveMux: func(mux *http.ServeMux) {
				mux.HandleFunc("/checksums.txt", func(w http.ResponseWriter, r *http.Request) {
					w.Write([]byte(strings.Repeat("a", 64) + "  test.patch\n"))
				})
			},
			assets: func(serverURL string) []release.Asset {
				return []release.Asset{
					{Name: "checksums.txt", BrowserDownloadURL: serverURL + "/checksums.txt"},
				}
			},
			patchName:    "test.patch",
			expectErr:    true,
			errSubstring: "SHA-256 mismatch",
		},
		{
			name: "no checksum found in monolithic file",
			serveMux: func(mux *http.ServeMux) {
				mux.HandleFunc("/checksums.txt", func(w http.ResponseWriter, r *http.Request) {
					w.Write([]byte(expectedSHA256 + "  other.patch\n"))
				})
			},
			assets: func(serverURL string) []release.Asset {
				return []release.Asset{
					{Name: "checksums.txt", BrowserDownloadURL: serverURL + "/checksums.txt"},
				}
			},
			patchName:    "test.patch",
			expectErr:    true,
			errSubstring: "no checksum found",
		},
	}

	// We intercept requests globally to avoid Downloader's hostname allowlist restrictions on 127.0.0.1
	oldTransport := http.DefaultTransport
	http.DefaultTransport = &testTransport{mux: nil} // replaced later
	defer func() { http.DefaultTransport = oldTransport }()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := http.NewServeMux()
			tt.serveMux(mux)
			http.DefaultTransport.(*testTransport).mux = mux

			// We use https://example.com to bypass FindAssetURL's https check AND the downloader's hostname allowlist.
			// The request will be intercepted by testTransport.
			serverURL := "https://github.com"

			dl := &downloader.Downloader{
				TmpDir: t.TempDir(),
			}

			rel := &release.Release{
				TagName: "v1.0.0",
				Assets:  tt.assets(serverURL),
				DL:      dl,
			}

			step := patchStep{
				name:    tt.patchName,
				release: rel,
			}

			err := VerifyPatchChecksum(step, patchFile)
			if tt.expectErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if !strings.Contains(err.Error(), tt.errSubstring) {
					t.Errorf("expected error to contain %q, got: %v", tt.errSubstring, err)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			}
		})
	}
}

type testTransport struct {
	mux *http.ServeMux
}

func (t *testTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	w := httptest.NewRecorder()
	t.mux.ServeHTTP(w, req)
	return w.Result(), nil
}

func TestPatchUpgradeTest(t *testing.T) {
	t.Setenv("HOMEGREW_ALLOWED_HOSTS", "github.com")
	// Create dummy patch content and checksums
	patchContent := []byte("dummy-patch")
	h256 := sha256.Sum256(patchContent)
	expectedSHA256 := hex.EncodeToString(h256[:])

	// Create a temporary patch file
	tmpDir := t.TempDir()
	patchFile := filepath.Join(tmpDir, "test.patch")
	if err := os.WriteFile(patchFile, patchContent, 0644); err != nil {
		t.Fatalf("failed to write patch file: %v", err)
	}

	// We intercept requests globally
	oldTransport := http.DefaultTransport
	mux := http.NewServeMux()
	http.DefaultTransport = &testTransport{mux: mux}
	defer func() { http.DefaultTransport = oldTransport }()

	// Serve the checksum file for all patches
	mux.HandleFunc("/checksums.txt", func(w http.ResponseWriter, r *http.Request) {
		lines := []string{
			expectedSHA256 + "  grew_Darwin_x86_64_v1.0.0_to_v1.1.0.patch",
			expectedSHA256 + "  grew_Darwin_x86_64_v1.1.0_to_v1.2.0.patch",
		}
		w.Write([]byte(strings.Join(lines, "\n") + "\n"))
	})

	// Serve the patch files
	mux.HandleFunc("/grew_Darwin_x86_64_v1.0.0_to_v1.1.0.patch", func(w http.ResponseWriter, r *http.Request) {
		w.Write(patchContent)
	})
	mux.HandleFunc("/grew_Darwin_x86_64_v1.1.0_to_v1.2.0.patch", func(w http.ResponseWriter, r *http.Request) {
		w.Write(patchContent)
	})

	serverURL := "https://github.com"
	dl := &downloader.Downloader{TmpDir: t.TempDir()}

	releases := []release.Release{
		{
			TagName: "v1.2.0",
			Assets: []release.Asset{
				{Name: "grew_Darwin_x86_64_v1.1.0_to_v1.2.0.patch", BrowserDownloadURL: serverURL + "/grew_Darwin_x86_64_v1.1.0_to_v1.2.0.patch"},
				{Name: "checksums.txt", BrowserDownloadURL: serverURL + "/checksums.txt"},
			},
			DL: dl,
		},
		{
			TagName: "v1.1.0",
			Assets: []release.Asset{
				{Name: "grew_Darwin_x86_64_v1.0.0_to_v1.1.0.patch", BrowserDownloadURL: serverURL + "/grew_Darwin_x86_64_v1.0.0_to_v1.1.0.patch"},
				{Name: "checksums.txt", BrowserDownloadURL: serverURL + "/checksums.txt"},
			},
			DL: dl,
		},
		{
			TagName: "v1.0.0",
			Assets:  []release.Asset{},
			DL:      dl,
		},
	}

	t.Run("successful upgrade path", func(t *testing.T) {
		osName, archName := normalizePlatformForTest()
		patch1 := "grew_" + osName + "_" + archName + "_v1.0.0_to_v1.1.0.patch"
		patch2 := "grew_" + osName + "_" + archName + "_v1.1.0_to_v1.2.0.patch"

		t.Logf("Platform: %s %s", osName, archName)
		t.Logf("patch1: %s, patch2: %s", patch1, patch2)
		t.Logf("ParsePatchVersion(patch1): %q", release.ParsePatchVersion(patch1))
		t.Logf("ParsePatchVersion(patch2): %q", release.ParsePatchVersion(patch2))

		dynamicMux := http.NewServeMux()
		dynamicMux.HandleFunc("/checksums.txt", func(w http.ResponseWriter, r *http.Request) {
			lines := []string{
				expectedSHA256 + "  " + patch1,
				expectedSHA256 + "  " + patch2,
			}
			w.Write([]byte(strings.Join(lines, "\n") + "\n"))
		})
		dynamicMux.HandleFunc("/"+patch1, func(w http.ResponseWriter, r *http.Request) { w.Write(patchContent) })
		dynamicMux.HandleFunc("/"+patch2, func(w http.ResponseWriter, r *http.Request) { w.Write(patchContent) })
		http.DefaultTransport.(*testTransport).mux = dynamicMux

		dynamicReleases := []release.Release{
			{
				TagName: "v1.2.0",
				Assets: []release.Asset{
					{Name: patch2, BrowserDownloadURL: serverURL + "/" + patch2},
					{Name: "checksums.txt", BrowserDownloadURL: serverURL + "/checksums.txt"},
				},
				DL: dl,
			},
			{
				TagName: "v1.1.0",
				Assets: []release.Asset{
					{Name: patch1, BrowserDownloadURL: serverURL + "/" + patch1},
					{Name: "checksums.txt", BrowserDownloadURL: serverURL + "/checksums.txt"},
				},
				DL: dl,
			},
			{
				TagName: "v1.0.0",
				Assets:  []release.Asset{},
				DL:      dl,
			},
		}

		_, err := TestPatchUpgrade("v1.0.0", "v1.2.0", dynamicReleases)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("no path found", func(t *testing.T) {
		_, err := TestPatchUpgrade("v0.9.0", "v1.2.0", releases)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "no patch path found") {
			t.Errorf("expected 'no patch path found', got: %v", err)
		}
	})
}

func normalizePlatformForTest() (osName, archName string) {
	osName = runtime.GOOS
	archName = runtime.GOARCH
	switch osName {
	case "darwin":
		osName = "Darwin"
	}
	switch archName {
	case "amd64":
		archName = "x86_64"
	}
	// Do not override arm64
	return
}
