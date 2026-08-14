/*
CS2 Analyser Tool version.
*/
package cmd

import (
	"fmt"
	"runtime/debug"

	"github.com/spf13/cobra"
)

var CS2AnalyserVersion string

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Display application version information.",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Fprintf(cmd.OutOrStdout(), "CS2 Analyser Tool version %s\n", currentVersion())
	},
}

func currentVersion() string {
	buildInfo, _ := debug.ReadBuildInfo()
	return resolveVersion(CS2AnalyserVersion, buildInfo)
}

func resolveVersion(linkerVersion string, buildInfo *debug.BuildInfo) string {
	if linkerVersion != "" {
		return linkerVersion
	}
	if buildInfo != nil && buildInfo.Main.Version != "" && buildInfo.Main.Version != "(devel)" {
		return buildInfo.Main.Version
	}
	return "dev"
}
