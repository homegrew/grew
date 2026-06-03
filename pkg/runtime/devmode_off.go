//go:build !devmode

package runtime

// DevMode enables developer features such as user-local installs (~/.homegrew).
// Release builds have this set to false; use go build -tags devmode to enable.
const DevMode = false
