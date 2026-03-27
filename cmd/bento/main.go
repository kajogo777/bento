package main

import (
	"os"

	"github.com/kajogo777/bento/internal/cli"
)

var version = "dev"

func main() {
	rootCmd := cli.NewRootCmd(version)
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
