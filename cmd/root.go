/*
Copyright © 2023 Tauã Almeida tauan96@gmail.com
*/
package cmd

import (
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "cs2-analyser-tool",
	Short: "Analyse Counter-Strike 2 demos.",
	Long:  "Parse Counter-Strike 2 demo files, display player and team statistics, export results, and review local match history.",
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Help()
	},
}

func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
