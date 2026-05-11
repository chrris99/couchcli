package main

import (
	"os"

	"github.com/chrris99/couchcli/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
