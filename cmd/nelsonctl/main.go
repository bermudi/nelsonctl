package main

import (
	"context"
	"os"
)

func main() {
	cwd, err := os.Getwd()
	if err != nil {
		os.Exit(1)
	}
	os.Exit(runCLI(context.Background(), os.Args[1:], cwd, os.Stdout, os.Stderr))
}
