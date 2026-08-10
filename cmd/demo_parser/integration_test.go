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

// demoFixture is a real CS2 demo the parser is run against. The demos are
// not committed to the repo (they are tens of megabytes); fetch them with
// `make download-test-demos`. Each is pinned by SHA-256 so the bytes cannot
// change underneath the golden files.
type demoFixture struct {
	name   string
	demo   string
	sha256 string
	golden string
}

// The two fixtures cover the two ways a CS2 demo reports round MVPs, which
// matters because the parser has to handle both:
//
//   - mirage is from January 2024 and still carries round_mvp game events,
//     so it exercises the RoundMVPAnnouncement handler.
//   - ancient is a later Premier match with no round_mvp events at all, so
//     its MVP counts can only come from the scoreboard entity property.
//     Dropping syncScoreboardMVPs zeroes this golden's MVPs.
//
// Neither demo contains shotgun damage, so the per-event damage cap for
// double-reported pellets cannot be locked from here; it is covered by
// TestShotgunPelletsDoNotDoubleCountDamage instead. A fixture with shotgun
// hits would close that end to end, tracked in issue #12.
var demoFixtures = []demoFixture{
	{
		name:   "mirage_round_mvp_events",
		demo:   "testdata/mirage.dem",
		sha256: "84a1a4191302bdd2a3bbb5a727842093744b1fb1a228aeec630369e44b622cb2",
		golden: "testdata/golden_mirage.json",
	},
	{
		name:   "ancient_scoreboard_mvps",
		demo:   "testdata/ancient.dem",
		sha256: "b29a9cb537a181deef97b15cfed10ee722a37999644a27bb2226fdd77a1337fc",
		golden: "testdata/golden_ancient.json",
	},
}

var updateGolden = flag.Bool("update", false, "rewrite the golden files from current ProcessDemo output")

// TestProcessDemoGolden runs the full parser pipeline on each fixture demo
// and compares the marshalled ProcessedDemo against its golden file. The
// golden output is a snapshot of behaviour that was validated against the
// in-game scoreboard (see issue #9), so any diff here means a stat
// regression, not a test problem. To accept an intentional behaviour
// change, regenerate with:
//
//	go test ./cmd/demo_parser -run TestProcessDemoGolden -update
func TestProcessDemoGolden(t *testing.T) {
	for _, f := range demoFixtures {
		t.Run(f.name, func(t *testing.T) {
			demoBytes, err := os.ReadFile(f.demo)
			if os.IsNotExist(err) {
				// CI sets REQUIRE_TEST_DEMO so a fixture that silently
				// fails to land in the expected place is a red build
				// rather than a skip that leaves the integration test
				// unrun and unnoticed.
				if os.Getenv("REQUIRE_TEST_DEMO") != "" {
					t.Fatalf("fixture %s is missing and REQUIRE_TEST_DEMO is set", f.demo)
				}
				t.Skipf("fixture %s not present, run `make download-test-demos` to fetch it", f.demo)
			}
			if err != nil {
				t.Fatalf("reading fixture: %v", err)
			}
			sum := sha256.Sum256(demoBytes)
			if got := hex.EncodeToString(sum[:]); got != f.sha256 {
				t.Fatalf("fixture %s has sha256 %s, want %s; delete it and run `make download-test-demos` again",
					f.demo, got, f.sha256)
			}

			result, err := ProcessDemo(f.demo)
			if err != nil {
				t.Fatalf("ProcessDemo: %v", err)
			}

			got, err := json.MarshalIndent(result, "", "  ")
			if err != nil {
				t.Fatalf("marshalling result: %v", err)
			}
			got = append(got, '\n')

			if *updateGolden {
				if err := os.WriteFile(f.golden, got, 0o644); err != nil {
					t.Fatalf("writing golden file: %v", err)
				}
				t.Logf("golden file %s updated", f.golden)
				return
			}

			want, err := os.ReadFile(f.golden)
			if err != nil {
				t.Fatalf("reading golden file: %v", err)
			}
			if diff := diffLines(string(want), string(got)); diff != "" {
				t.Errorf("ProcessDemo output drifted from %s:\n%s", f.golden, diff)
			}
		})
	}
}

// diffLines reports the lines where want and got differ, in the style of
// `diff`: "-" for golden-only lines, "+" for lines the parser produced.
// The alignment comes from a longest-common-subsequence walk, so adding or
// removing a field shows up as that one insertion instead of shifting every
// later line into a false mismatch. Returns "" when the inputs are equal.
func diffLines(want, got string) string {
	if want == got {
		return ""
	}
	const maxReported = 25
	a := strings.Split(want, "\n")
	b := strings.Split(got, "\n")

	// lcs[i][j] is the length of the longest common subsequence of a[i:]
	// and b[j:], which is what lets the walk below prefer real matches.
	lcs := make([][]int, len(a)+1)
	for i := range lcs {
		lcs[i] = make([]int, len(b)+1)
	}
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}

	var out strings.Builder
	var reported, total int
	report := func(format string, args ...any) {
		total++
		if reported < maxReported {
			fmt.Fprintf(&out, format, args...)
			reported++
		}
	}
	for i, j := 0, 0; i < len(a) || j < len(b); {
		switch {
		case i < len(a) && j < len(b) && a[i] == b[j]:
			i++
			j++
		case j == len(b) || (i < len(a) && lcs[i+1][j] >= lcs[i][j+1]):
			report("  - golden:%d: %s\n", i+1, a[i])
			i++
		default:
			report("  + got:%d:    %s\n", j+1, b[j])
			j++
		}
	}
	if total > reported {
		fmt.Fprintf(&out, "  ... %d more differing lines\n", total-reported)
	}
	return out.String()
}
