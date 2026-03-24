//go:build devmode

package runtime

// DevMode enables developer features such as user-local installs (~/.homegrew).
// This is set at compile time: go build -tags devmode
const DevMode = true
