/*
CS2 Analyser Tool version.
*/
package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var CS2AnalyserVersion string

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Display application version information.",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("CS2 Analyser Tool version %v\n", CS2AnalyserVersion)
	},
}
