// Package config provides the central path management and configuration discovery
// for grew. It handles determining the installation prefix, user-specific
// directories, and ensuring the environment is correctly initialized.
//
// The package is designed to be a single source of truth for all filesystem
// paths used by the application, ensuring consistency across different commands
// and components. It also provides validation to ensure that operations remain
// within the intended prefix.
package config
