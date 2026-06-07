package receipt

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/homegrew/grew/pkg/fsutil"
	"github.com/homegrew/grew/pkg/safepath"
)

// ReceiptFile is the name of the receipt file stored at the root of each keg
// directory (e.g. <prefix>/Cellar/jq/1.6/INSTALL_RECEIPT.json).
const ReceiptFile = "INSTALL_RECEIPT.json"

// Receipt records provenance and build metadata for an installed formula.
// It is written once at the end of installation and is treated as
// supplemental — integrity verification uses .MANIFEST.json instead, and
// the receipt is explicitly excluded from those checks to avoid false
// positives caused by the file being created after the snapshot.
type Receipt struct {
	// Name is the formula name as it appears in the tap definition.
	Name string `json:"name"`

	// Version is the installed version string (e.g. "1.6").
	Version string `json:"version"`

	// BuiltFromSource is true when the formula was compiled locally rather
	// than poured from a pre-built bottle.
	BuiltFromSource bool `json:"built_from_source"`

	// PouredFromBottle is true when a pre-built binary bottle was used.
	PouredFromBottle bool `json:"poured_from_bottle"`

	// InstalledAt is the UTC timestamp recorded at the end of installation.
	InstalledAt time.Time `json:"installed_at"`

	// Dependencies lists the direct formula dependencies declared by the
	// formula at install time.
	Dependencies []string `json:"dependencies"`

	// RuntimeDependencies lists only the subset of Dependencies that are
	// needed at runtime (as opposed to build-only deps). Omitted when empty.
	RuntimeDependencies []string `json:"runtime_dependencies,omitempty"`

	// Compiler records the compiler used for a source build (e.g. "clang").
	// Omitted for bottle installs.
	Compiler string `json:"compiler,omitempty"`

	// BuildOptions lists any custom flags passed to the build step.
	// Omitted when none were provided.
	BuildOptions []string `json:"build_options,omitempty"`

	// InstalledOnRequest is true when the user explicitly asked for this
	// formula (e.g. via `grew install`). False means it was pulled in
	// automatically as a dependency. This field drives `grew leaves` and
	// `grew autoremove`.
	InstalledOnRequest bool `json:"installed_on_request"`
}

// Save atomically writes the receipt to kegPath/INSTALL_RECEIPT.json.
func Save(r *Receipt, kegPath string) error {
	kegPath, err := safepath.CleanPath(kegPath)
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal receipt: %w", err)
	}
	data = append(data, '\n')

	dest := filepath.Join(kegPath, ReceiptFile)

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return fmt.Errorf("create directory for receipt: %w", err)
	}

	if err := fsutil.WriteFileAtomic(dest, data, 0644); err != nil {
		return fmt.Errorf("write receipt: %w", err)
	}
	return nil
}

// Load reads and parses the receipt from kegPath/INSTALL_RECEIPT.json.
func Load(kegPath string) (*Receipt, error) {
	kegPath, err := safepath.CleanPath(kegPath)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(kegPath, ReceiptFile))
	if err != nil {
		return nil, fmt.Errorf("read receipt: %w", err)
	}
	var r Receipt
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("parse receipt: %w", err)
	}
	return &r, nil
}

// Exists returns true if a receipt exists for the given keg.
func Exists(kegPath string) bool {
	kegPath, err := safepath.CleanPath(kegPath)
	if err != nil {
		return false
	}
	_, err = os.Stat(filepath.Join(kegPath, ReceiptFile))
	return err == nil
}
