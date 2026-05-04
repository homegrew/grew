package create

import (
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/homegrew/grew/internal/context"
	"github.com/homegrew/grew/internal/downloader"
	"github.com/homegrew/grew/internal/formula"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var Command = &cobra.Command{
	Use:   "create <url>",
	Short: "Create a new formula from a URL",
	Long: `Create a new formula by downloading a URL, calculating its SHA256,
and inferring the package name and version. Outputs a YAML template.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runCreate(args[0])
	},
}

func runCreate(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	name, version := inferNameVersion(u)
	fmt.Fprintf(os.Stderr, "==> Inferred name: %s, version: %s\n", name, version)

	ctx, err := context.NewInstallContext()
	if err != nil {
		return err
	}
	defer ctx.Close()

	filename := filepath.Base(u.Path)
	if filename == "" || filename == "." || filename == "/" {
		filename = "download.tar.gz"
	}

	fmt.Fprintf(os.Stderr, "==> Downloading %s...\n", rawURL)
	path, err := ctx.DL.Download(rawURL, filename)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}

	sha256, err := downloader.ComputeSHA256(path)
	if err != nil {
		return fmt.Errorf("hash calculation failed: %w", err)
	}

	f := &formula.Formula{
		Name:        name,
		Version:     version,
		Description: "",
		Homepage:    inferHomepage(u),
		License:     "",
		URL: map[string]string{
			"darwin_arm64": rawURL,
			"linux_amd64":  rawURL,
		},
		SHA256: map[string]string{
			"darwin_arm64": sha256,
			"linux_amd64":  sha256,
		},
		Install: formula.InstallSpec{
			Type: "archive",
		},
	}

	data, err := yaml.Marshal(f)
	if err != nil {
		return err
	}

	fmt.Println(string(data))
	fmt.Fprintf(os.Stderr, "==> Created formula template for %s\n", name)
	return nil
}

func inferNameVersion(u *url.URL) (string, string) {
	base := filepath.Base(u.Path)
	// Strip extensions
	for _, ext := range []string{".tar.gz", ".tar.xz", ".tar.bz2", ".zip", ".tgz", ".tar"} {
		if strings.HasSuffix(base, ext) {
			base = strings.TrimSuffix(base, ext)
			break
		}
	}

	// Try to find a version-like string (e.g. -1.2.3, _1.2.3, @1.2.3)
	re := regexp.MustCompile(`[-_@](v?\d+\.\d+.*)$`)
	if m := re.FindStringSubmatch(base); m != nil {
		name := strings.TrimSuffix(base, m[0])
		version := strings.TrimPrefix(m[1], "v")
		return name, version
	}

	// Fallback for GitHub archives: repo/archive/refs/tags/v1.0.0.tar.gz
	if strings.Contains(u.Path, "/archive/refs/tags/") {
		parts := strings.Split(u.Path, "/")
		// /owner/repo/archive/refs/tags/v1.0.0.tar.gz
		if len(parts) >= 3 {
			name := parts[2]
			version := strings.TrimPrefix(filepath.Base(u.Path), "v")
			version = strings.TrimSuffix(version, ".tar.gz")
			return name, version
		}
	}

	return base, "0.1.0"
}

func inferHomepage(u *url.URL) string {
	if u.Host == "github.com" {
		parts := strings.Split(u.Path, "/")
		if len(parts) >= 3 {
			return "https://github.com/" + parts[1] + "/" + parts[2]
		}
	}
	return u.Scheme + "://" + u.Host
}
