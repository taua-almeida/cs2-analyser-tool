package demoparser

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"testing"
)

// The fixture is a real CS2 Premier demo (de_mirage, 10 rounds) taken from
// the MIT-licensed LaihoE/demoparser test data, pinned to an immutable
// commit so the bytes can never change underneath us. It is not committed
// to the repo; fetch it with `make download-test-demo`.
const (
	fixtureDemoPath   = "testdata/test_demo.dem"
	fixtureDemoSHA256 = "84a1a4191302bdd2a3bbb5a727842093744b1fb1a228aeec630369e44b622cb2"
	goldenPath        = "testdata/golden_test_demo.json"
)

var updateGolden = flag.Bool("update", false, "rewrite the golden file from current ProcessDemo output")

// TestProcessDemoGolden runs the full parser pipeline on the fixture demo
// and compares the marshalled ProcessedDemo against the golden file. The
// golden output was validated against the in-game scoreboard convention
// (see issue #9), so any diff here means a stat regression, not a test
// problem. To accept an intentional behaviour change, regenerate with:
//
//	go test ./cmd/demo_parser -run TestProcessDemoGolden -update
func TestProcessDemoGolden(t *testing.T) {
	demoBytes, err := os.ReadFile(fixtureDemoPath)
	if os.IsNotExist(err) {
		t.Skipf("fixture %s not present, run `make download-test-demo` to fetch it", fixtureDemoPath)
	}
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}
	sum := sha256.Sum256(demoBytes)
	if got := hex.EncodeToString(sum[:]); got != fixtureDemoSHA256 {
		t.Fatalf("fixture %s has sha256 %s, want %s; delete it and run `make download-test-demo` again",
			fixtureDemoPath, got, fixtureDemoSHA256)
	}

	result, err := ProcessDemo(fixtureDemoPath)
	if err != nil {
		t.Fatalf("ProcessDemo: %v", err)
	}

	got, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		t.Fatalf("marshalling result: %v", err)
	}
	got = append(got, '\n')

	if *updateGolden {
		if err := os.WriteFile(goldenPath, got, 0o644); err != nil {
			t.Fatalf("writing golden file: %v", err)
		}
		t.Logf("golden file %s updated", goldenPath)
		return
	}

	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("reading golden file: %v", err)
	}
	if diff := diffLines(string(want), string(got)); diff != "" {
		t.Errorf("ProcessDemo output drifted from %s:\n%s", goldenPath, diff)
	}
}

// diffLines reports the lines where want and got differ, with line numbers,
// so a stat drift points straight at the changed fields. Returns "" when
// the inputs are identical.
func diffLines(want, got string) string {
	if want == got {
		return ""
	}
	const maxReported = 25
	wantLines := strings.Split(want, "\n")
	gotLines := strings.Split(got, "\n")

	var b strings.Builder
	reported := 0
	for i := 0; i < len(wantLines) || i < len(gotLines); i++ {
		var w, g string
		if i < len(wantLines) {
			w = wantLines[i]
		}
		if i < len(gotLines) {
			g = gotLines[i]
		}
		if w == g {
			continue
		}
		if reported == maxReported {
			b.WriteString("  ... more lines differ, truncated\n")
			break
		}
		fmt.Fprintf(&b, "  line %d:\n    golden: %s\n    got:    %s\n", i+1, w, g)
		reported++
	}
	if len(wantLines) != len(gotLines) {
		fmt.Fprintf(&b, "  line count: golden %d, got %d\n", len(wantLines), len(gotLines))
	}
	return b.String()
}
