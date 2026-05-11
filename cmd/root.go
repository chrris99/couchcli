package cmd

import (
	"github.com/spf13/cobra"
)

var (
	version = "dev"

	jsonOutput   bool
	prettyOutput bool
)

var rootCmd = &cobra.Command{
	Use:     "couch",
	Short:   "Control smart TVs from your terminal",
	Long:    "couch is a CLI tool for discovering and controlling smart TVs over your local network.",
	Version: version,
}

func init() {
	rootCmd.PersistentFlags().BoolVar(&jsonOutput, "json", false, "force JSON output")
	rootCmd.PersistentFlags().BoolVar(&prettyOutput, "pretty", false, "force table output")
}

func Execute() error {
	return rootCmd.Execute()
}
