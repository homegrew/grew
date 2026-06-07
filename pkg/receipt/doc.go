// Package receipt manages INSTALL_RECEIPT.json files stored inside each
// installed keg.
//
// A receipt captures provenance and build metadata that is not covered by the
// integrity snapshot (.MANIFEST.json): who installed the formula, when, how it
// was built, which dependencies were present, and — critically — whether it was
// an explicit user request or an automatic dependency pull-in
// (InstalledOnRequest). That last field is the primary input for
// `grew leaves` and `grew autoremove`.
//
// The receipt is written atomically at the end of every installation via Save,
// and is intentionally excluded from grew's integrity checks so that its
// creation after the snapshot does not trigger false tamper alerts.
//
// Typical usage:
//
//	r := &receipt.Receipt{
//	    Name:               "jq",
//	    Version:            "1.6",
//	    PouredFromBottle:   true,
//	    InstalledAt:        time.Now().UTC(),
//	    InstalledOnRequest: true,
//	}
//	if err := receipt.Save(r, kegPath); err != nil { ... }
//
//	r, err := receipt.Load(kegPath)
package receipt
