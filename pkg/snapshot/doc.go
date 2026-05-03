// Package snapshot provides mechanisms for capturing, storing, and verifying
// cryptographic manifests of installed software packages (kegs).
//
// This package is central to grew's integrity and reproducibility guarantees.
// When a package is installed, the snapshot package is used to traverse the
// extracted files (the "keg"), compute cryptographic hashes (SHA-256 and SHA-512)
// of every file, and record their sizes, permissions, and symlink targets.
// This data is saved into a JSON manifest file (.MANIFEST.json) at the root
// of the keg.
//
// # Core Concepts
//
//   - Manifest: A structured record containing provenance data (where the package
//     came from, how it was installed) and a comprehensive file inventory.
//   - Capture: The process of walking a directory structure, hashing files, and
//     generating a Manifest in memory.
//   - Verify: The process of loading an existing Manifest and comparing it against
//     the current state of the filesystem to detect missing, modified, or added files.
//
// # Usage
//
// Capturing a new snapshot after installation:
//
//	meta := snapshot.InstallMeta{
//	    Platform:       "darwin_arm64",
//	    DownloadURL:    "https://example.com/software.tar.gz",
//	    DownloadSHA256: "...",
//	}
//	manifest, err := snapshot.Capture("software", "1.0.0", "/path/to/keg", meta)
//	if err != nil {
//	    // handle error
//	}
//	err = snapshot.Save(manifest, "/path/to/keg")
//
// Verifying an existing installation:
//
//	if snapshot.Exists("/path/to/keg") {
//	    result, err := snapshot.Verify("/path/to/keg")
//	    if err != nil {
//	        // handle error
//	    }
//	    if !result.OK {
//	        fmt.Printf("Verification failed: %v modified, %v missing, %v added\n",
//	            len(result.Modified), len(result.Missing), len(result.Added))
//	    }
//	}
//
// # Security Considerations
//
// The functions in this package perform safety checks to prevent directory
// traversal attacks. The cleanKegPath function ensures that the provided keg paths
// do not attempt to escape the intended directory boundaries (e.g., via "../").
//
// During verification, certain metadata files generated *after* the initial
// installation snapshot (such as INSTALL_RECEIPT.json) are explicitly ignored
// to prevent false-positive tamper alerts.
package snapshot
