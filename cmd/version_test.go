package cmd

import (
	"bytes"
	"runtime/debug"
	"testing"
)

func TestResolveVersion(t *testing.T) {
	tests := []struct {
		name          string
		linkerVersion string
		moduleVersion string
		buildInfo     bool
		want          string
	}{
		{name: "linker version wins", linkerVersion: "v0.1.0", moduleVersion: "v9.9.9", buildInfo: true, want: "v0.1.0"},
		{name: "installed module version", moduleVersion: "v0.2.0", buildInfo: true, want: "v0.2.0"},
		{name: "development module", moduleVersion: "(devel)", buildInfo: true, want: "dev"},
		{name: "empty module version", buildInfo: true, want: "dev"},
		{name: "missing build info", want: "dev"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var buildInfo *debug.BuildInfo
			if test.buildInfo {
				buildInfo = &debug.BuildInfo{Main: debug.Module{Version: test.moduleVersion}}
			}
			if got := resolveVersion(test.linkerVersion, buildInfo); got != test.want {
				t.Fatalf("resolveVersion(%q, module %q) = %q, want %q", test.linkerVersion, test.moduleVersion, got, test.want)
			}
		})
	}
}

func TestVersionCommandOutput(t *testing.T) {
	oldVersion := CS2AnalyserVersion
	CS2AnalyserVersion = "v0.1.0"
	t.Cleanup(func() {
		CS2AnalyserVersion = oldVersion
		// rootCmd has no parent or configured writer, so nil restores its default.
		rootCmd.SetOut(nil)
		// A nil override restores Cobra's default of reading os.Args[1:].
		rootCmd.SetArgs(nil)
	})

	var output bytes.Buffer
	rootCmd.SetOut(&output)
	rootCmd.SetArgs([]string{"version"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("executing version command: %v", err)
	}

	const want = "cs2-analyser-tool version v0.1.0\n"
	if got := output.String(); got != want {
		t.Fatalf("version output = %q, want %q", got, want)
	}
}
