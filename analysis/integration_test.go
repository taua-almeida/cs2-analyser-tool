package analysis

import (
	"context"
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
// not committed to the repo. Public fixtures can be fetched with
// `make download-test-demos`; Inferno must be supplied locally. Each is pinned
// by SHA-256 so the bytes cannot change underneath the golden files.
type demoFixture struct {
	name                 string
	demo                 string
	sha256               string
	golden               string
	requiredBy           string
	expectsUnusedUtility bool
	expectsSideSwap      bool
}

const (
	requirePublicTestDemoEnv  = "REQUIRE_TEST_DEMO"
	requirePrivateTestDemoEnv = "REQUIRE_PRIVATE_TEST_DEMO"
)

// The fixtures cover the two ways a CS2 demo reports round MVPs, which
// matters because the parser has to handle both, plus event sequences the
// shorter matches do not contain:
//
//   - mirage is from January 2024 and still carries round_mvp game events,
//     so it exercises the RoundMVPAnnouncement handler.
//   - ancient is a later Premier match with no round_mvp events at all, so
//     its MVP counts can only come from the scoreboard entity property.
//     Dropping the staged scoreboard-MVP snapshot zeroes this golden's MVPs.
//   - inferno contains XM1014 pellet events whose reported damage exceeds
//     the victim's real health loss, reaches halftime, and has a player join
//     between RoundStart and the end of freeze time. It exercises the damage
//     cap, side splits after the swap, and freeze-time roster update through
//     a real parse.
var demoFixtures = []demoFixture{
	{
		name:                 "mirage_round_mvp_events",
		demo:                 "testdata/mirage.dem",
		sha256:               "84a1a4191302bdd2a3bbb5a727842093744b1fb1a228aeec630369e44b622cb2",
		golden:               "testdata/golden_mirage.json",
		requiredBy:           requirePublicTestDemoEnv,
		expectsUnusedUtility: true,
	},
	{
		name:       "ancient_scoreboard_mvps",
		demo:       "testdata/ancient.dem",
		sha256:     "b29a9cb537a181deef97b15cfed10ee722a37999644a27bb2226fdd77a1337fc",
		golden:     "testdata/golden_ancient.json",
		requiredBy: requirePublicTestDemoEnv,
	},
	{
		name:            "inferno_shotgun_halftime_and_freeze_join",
		demo:            "testdata/inferno-shotgun.dem",
		sha256:          "095625b47c2cc6ace12414a6bbc987ea254904d969ae39fb95c7d54e085f7f93",
		golden:          "testdata/golden_inferno_shotgun.json",
		requiredBy:      requirePrivateTestDemoEnv,
		expectsSideSwap: true,
	},
}

var updateGolden = flag.Bool("update", false, "rewrite the golden files from current analysis output")

// TestAnalyseGolden runs the full parser pipeline on each fixture demo
// and compares the marshalled MapAnalysis against its golden file. The
// golden output is a snapshot of behaviour that was validated against the
// in-game scoreboard (see issue #9), so any diff here means a stat
// regression, not a test problem. To accept an intentional behaviour
// change, regenerate with:
//
//	go test ./analysis -run TestAnalyseGolden -update
func TestAnalyseGolden(t *testing.T) {
	for _, f := range demoFixtures {
		t.Run(f.name, func(t *testing.T) {
			demoBytes, err := os.ReadFile(f.demo)
			if os.IsNotExist(err) {
				// CI sets the fixture's requirement flag after provisioning,
				// turning a missing expected demo into a failure instead of an
				// unnoticed skip.
				if fixtureIsRequired(f, os.Getenv) {
					t.Fatalf("fixture %s is missing and %s is set", f.demo, f.requiredBy)
				}
				t.Skipf("fixture %s not present; see testdata/README.md", f.demo)
			}
			if err != nil {
				t.Fatalf("reading fixture: %v", err)
			}
			sum := sha256.Sum256(demoBytes)
			if got := hex.EncodeToString(sum[:]); got != f.sha256 {
				t.Fatalf("fixture %s has sha256 %s, want %s; replace it with the expected demo",
					f.demo, got, f.sha256)
			}

			result, err := AnalyseFile(context.Background(), f.demo)
			if err != nil {
				t.Fatalf("AnalyseFile: %v", err)
			}
			if f.expectsUnusedUtility {
				// A positive aggregate proves Kill still exposes pre-death
				// grenade inventory. The golden below pins every exact value;
				// this assertion documents the prerequisite independently.
				observed := false
				for _, player := range result.Players {
					if player.UtilityStats.UnusedUtilityValue > 0 {
						observed = true
						break
					}
				}
				if !observed {
					t.Fatal("fixture produced no unused utility; Kill may no longer expose pre-death inventory")
				}
			}
			if f.expectsSideSwap {
				if result.Map.TotalRounds < 13 {
					t.Fatalf("fixture has %d rounds, want at least 13 to cross halftime", result.Map.TotalRounds)
				}
				observed := false
				for _, player := range result.Players {
					if player.SideStats.Rounds.CT > 0 && player.SideStats.Rounds.T > 0 {
						observed = true
						break
					}
				}
				if !observed {
					t.Fatal("fixture produced no player with rounds on both sides")
				}
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
				t.Errorf("analysis output drifted from %s:\n%s", f.golden, diff)
			}
		})
	}
}

func fixtureIsRequired(fixture demoFixture, getenv func(string) string) bool {
	return fixture.requiredBy != "" && getenv(fixture.requiredBy) != ""
}

func TestFixtureRequirementFlagsAreIndependent(t *testing.T) {
	publicFixture := demoFixture{requiredBy: requirePublicTestDemoEnv}
	privateFixture := demoFixture{requiredBy: requirePrivateTestDemoEnv}

	for _, test := range []struct {
		name    string
		fixture demoFixture
		env     map[string]string
		want    bool
	}{
		{name: "public required", fixture: publicFixture, env: map[string]string{requirePublicTestDemoEnv: "1"}, want: true},
		{name: "private required", fixture: privateFixture, env: map[string]string{requirePrivateTestDemoEnv: "1"}, want: true},
		{name: "public flag does not require private", fixture: privateFixture, env: map[string]string{requirePublicTestDemoEnv: "1"}},
		{name: "private flag does not require public", fixture: publicFixture, env: map[string]string{requirePrivateTestDemoEnv: "1"}},
		{name: "unset", fixture: privateFixture},
	} {
		t.Run(test.name, func(t *testing.T) {
			getenv := func(name string) string { return test.env[name] }
			if got := fixtureIsRequired(test.fixture, getenv); got != test.want {
				t.Fatalf("fixtureIsRequired() = %t, want %t", got, test.want)
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
