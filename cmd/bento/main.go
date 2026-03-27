package main

import (
	"fmt"
	"os"

	"github.com/bentoci/bento/internal/cli"
)

var version = "dev"

func main() {
	rootCmd := cli.NewRootCmd(version)
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
