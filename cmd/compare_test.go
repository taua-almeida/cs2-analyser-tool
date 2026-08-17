package cmd

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/taua-almeida/cs2-analyser-tool/internal/history"
)

// compareFixtureDB stores two premier maps and one competitive map for the
// same players, the premier ones an hour apart.
func compareFixtureDB(t *testing.T) *history.DB {
	t.Helper()
	db := newHistoryDB(t)
	storeTestMatch(t, db, 0, storedDemo("de_mirage", "premier", defaultNames()), testAnalysedAt, nil)
	storeTestMatch(t, db, 1, storedDemo("de_ancient", "premier", defaultNames()), testAnalysedAt.Add(time.Hour), nil)
	storeTestMatch(t, db, 2, storedDemo("de_nuke", "competitive", defaultNames()), testAnalysedAt.Add(2*time.Hour), nil)
	return db
}

// TestRunCompareBySteamIDAndAlias pins the trend rendering for both identity
// forms: only the two premier maps count, the alias appears, and the totals
// row carries the exact additive aggregates — 40 kills over 28 deaths and
// 3800 damage over 42 rounds, never an average of per-map rates.
func TestRunCompareBySteamIDAndAlias(t *testing.T) {
	db := compareFixtureDB(t)
	for _, playerArg := range []string{fmt.Sprintf("%d", testSteamOne), "alpha"} {
		var out strings.Builder
		if err := runCompare(context.Background(), db, &out, playerArg); err != nil {
			t.Fatalf("runCompare(%q): %v", playerArg, err)
		}
		rendered := out.String()
		for _, want := range []string{
			fmt.Sprintf("Premier trend for SteamID %d over 2 maps (42 rounds)", testSteamOne),
			"Known as: Alpha",
			"de_mirage", "de_ancient",
			"1.429", // total K/D = 40/28
			"90.5",  // total ADR 3800/42 = 90.476 → 90.5
			"76.2",  // total KAST = 100*32/42 = 76.19 → 76.2
			"45.0",  // total HS% = 100*18/40
			"12:4",  // total entry = summed opening kills:deaths
			"66.7",  // opening success = 100*8/12
			"Totals are exact sums",
		} {
			if !strings.Contains(rendered, want) {
				t.Errorf("compare(%q) output lacks %q:\n%s", playerArg, want, rendered)
			}
		}
		if strings.Contains(rendered, "de_nuke") {
			t.Errorf("compare(%q) includes the competitive map:\n%s", playerArg, rendered)
		}
	}
}

// TestRunCompareAmbiguousAlias pins that an alias naming two stored SteamIDs
// surfaces the structured ambiguity with both candidates.
func TestRunCompareAmbiguousAlias(t *testing.T) {
	db := newHistoryDB(t)
	names := defaultNames()
	names[testSteamTwo] = "Alpha"
	storeTestMatch(t, db, 0, storedDemo("de_mirage", "premier", names), testAnalysedAt, nil)

	err := runCompare(context.Background(), db, &strings.Builder{}, "ALPHA")
	var ambiguous *history.AliasAmbiguityError
	if !errors.As(err, &ambiguous) {
		t.Fatalf("err = %v, want an AliasAmbiguityError", err)
	}
	if len(ambiguous.Candidates) != 2 {
		t.Errorf("candidates = %+v, want both SteamIDs", ambiguous.Candidates)
	}
	for _, id := range []uint64{testSteamOne, testSteamTwo} {
		if !strings.Contains(err.Error(), fmt.Sprintf("%d", id)) {
			t.Errorf("error %q does not name SteamID %d", err, id)
		}
	}
}

// TestRunCompareWithoutPremierMatches pins the friendly empty result: a
// resolvable player with no premier maps is a message, not an error.
func TestRunCompareWithoutPremierMatches(t *testing.T) {
	db := newHistoryDB(t)
	storeTestMatch(t, db, 0, storedDemo("de_nuke", "competitive", defaultNames()), testAnalysedAt, nil)

	var out strings.Builder
	if err := runCompare(context.Background(), db, &out, "Alpha"); err != nil {
		t.Fatalf("runCompare: %v", err)
	}
	if !strings.Contains(out.String(), "No stored premier matches") {
		t.Errorf("output %q lacks the empty-trend message", out.String())
	}
}

// TestRunCompareUnknownPlayer pins the failure for a name no stored match
// ever used.
func TestRunCompareUnknownPlayer(t *testing.T) {
	db := compareFixtureDB(t)
	err := runCompare(context.Background(), db, &strings.Builder{}, "NoSuchPlayer")
	if err == nil || !strings.Contains(err.Error(), "no stored player") {
		t.Fatalf("err = %v, want the unknown-player failure", err)
	}
}
