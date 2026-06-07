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

// ReceiptFile is the name of the receipt stored inside each keg.
const ReceiptFile = "INSTALL_RECEIPT.json"

// Receipt records metadata about an installation.
type Receipt struct {
	Name                string    `json:"name"`
	Version             string    `json:"version"`
	BuiltFromSource     bool      `json:"built_from_source"`
	PouredFromBottle    bool      `json:"poured_from_bottle"`
	InstalledAt         time.Time `json:"installed_at"`
	Dependencies        []string  `json:"dependencies"`
	RuntimeDependencies []string  `json:"runtime_dependencies,omitempty"`
	Compiler            string    `json:"compiler,omitempty"`
	BuildOptions        []string  `json:"build_options,omitempty"`
	InstalledOnRequest  bool      `json:"installed_on_request"`
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
