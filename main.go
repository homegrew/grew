package main

import (
	"fmt"
	"os"

	"github.com/homegrew/grew/internal/cmd"
)

func main() {
	if err := cmd.Run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "grew: %s\n", err)
		os.Exit(1)
	}
}
