package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/homegrew/grew/internal/cmd"
)

func main() {
	if err := cmd.Run(os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		fmt.Fprintf(os.Stderr, "grew: %s\n", err)
		os.Exit(1)
	}
}
