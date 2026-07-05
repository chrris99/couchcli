package cmd

import (
	"github.com/spf13/cobra"
)

var (
	version = "dev"

	jsonOutput bool
)

var rootCmd = &cobra.Command{
	Use:     "couch",
	Short:   "Control your home devices from your terminal",
	Long:    "couch is a CLI tool for discovering and controlling networked home devices, such as TVs, audio, lights and more, over your local network.",
	Version: version,
}

func init() {
	rootCmd.PersistentFlags().BoolVar(&jsonOutput, "json", false, "emit machine-readable JSON instead of formatted output")
}

func Root() *cobra.Command {
	return rootCmd
}
