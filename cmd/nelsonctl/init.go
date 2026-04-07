package main

import (
	"fmt"
	"io"

	"github.com/bermudi/nelsonctl/internal/config"
)

func runInit(stdout, stderr io.Writer, stdin io.Reader) int {
	cfg, path, err := config.Load()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	wizard := config.Wizard{In: stdin, Out: stdout}
	updated, err := wizard.Run(cfg)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	if err := config.Write(path, updated); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	fmt.Fprintf(stdout, "Wrote %s\n", path)
	fmt.Fprintln(stdout, "Credentials stay in environment variables and are not stored in config.yaml.")
	return 0
}
