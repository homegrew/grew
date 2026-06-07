// Package service manages background services defined by formula YAML
// definitions. It abstracts over the platform-native init system (launchd
// on macOS) so that the rest of grew interacts with a single, consistent API.
//
// # Service lifecycle
//
// A formula can declare a service block that specifies the command to run,
// optional working directory, keep-alive behaviour, and log paths. When
// a user calls `grew services start <formula>`, this package:
//
//  1. Resolves the run command, substituting {prefix}, {opt}, and {cellar}
//     placeholders with the actual prefix paths.
//  2. Generates a platform service file (a launchd .plist on macOS) and
//     writes it to ServiceDir.
//  3. Loads the service into the init system via the platform backend.
//
// Stop removes the service file and unloads the service. Restart is a
// stop followed by a start.
//
// # Naming convention
//
// Service labels follow the reverse-DNS convention "com.homegrew.<name>"
// (see [ServiceLabel]). On macOS these map directly to launchd job labels
// and the corresponding .plist file names.
//
// # Platform files
//
// The cross-platform interface is defined in service.go. Platform-specific
// implementations (file format, load/unload commands, status probing) live in
// service_darwin.go. On macOS, [DefaultManager] returns a Manager pointing at
// ~/Library/LaunchAgents, which is the standard per-user launchd directory.
package service
