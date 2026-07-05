package main

import (
	"context"
	"os"

	"github.com/charmbracelet/fang"

	"github.com/chrris99/couchcli/cmd"
)

func main() {
	if err := fang.Execute(context.Background(), cmd.Root()); err != nil {
		os.Exit(1)
	}
}
