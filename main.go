package main

import (
	"embed"
	"fmt"
	"os"

	"github.com/homegrew/grew/internal/cmd"
)

//go:embed taps/core/*.yaml taps/cask/*.yaml
var embeddedTaps embed.FS

func main() {
	if err := cmd.Run(os.Args[1:], embeddedTaps); err != nil {
		fmt.Fprintf(os.Stderr, "grew: %s\n", err)
		os.Exit(1)
	}
}
