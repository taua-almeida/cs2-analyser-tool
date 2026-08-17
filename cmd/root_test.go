package cmd

import (
	"bytes"
	"strings"
	"testing"
)

func TestCommandsUseCanonicalExecutableName(t *testing.T) {
	const canonicalName = "cs2-analyser-tool"
	if got := rootCmd.Name(); got != canonicalName {
		t.Fatalf("root command name = %q, want %q", got, canonicalName)
	}

	var output bytes.Buffer
	rootCmd.SetOut(&output)
	t.Cleanup(func() { rootCmd.SetOut(nil) })
	rootCmd.InitDefaultHelpFlag()
	rootCmd.Help()

	help := output.String()
	const usage = "Usage:\n  cs2-analyser-tool [flags]\n  cs2-analyser-tool [command]"
	if !strings.Contains(help, usage) {
		t.Fatalf("root help does not use the canonical executable name:\n%s", help)
	}

	const guidance = "Run 'cs2-analyser-tool history'"
	if !strings.Contains(analyseCmd.Long, guidance) {
		t.Fatalf("analyse help %q does not contain %q", analyseCmd.Long, guidance)
	}
}
