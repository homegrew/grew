//go:build !darwin

package cmd

import "github.com/homegrew/grew/internal/config"

func getPathHelperRoot(paths config.Paths) string {
	return ""
}
