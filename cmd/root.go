/*
Copyright © 2023 Tauã Almeida tauan96@gmail.com
*/
package cmd

import (
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "cs2-analyser",
	Short: "A CLI tool to analyse cs2 games demos",
	Long:  "CS2 Analyser Tool is a CLI tool that allows players and coaches to parse demos from CS2 games and analyse them.",
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
