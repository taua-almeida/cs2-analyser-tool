package cmd

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/taua-almeida/cs2-analyser-tool/analysis"
	"github.com/taua-almeida/cs2-analyser-tool/internal/history"
)

const (
	testSteamOne   uint64 = 76561198000000001
	testSteamTwo   uint64 = 76561198000000002
	testSteamThree uint64 = 76561198000000003
)

var testAnalysedAt = time.Date(2026, time.August, 3, 9, 30, 0, 0, time.UTC)

// newHistoryDB opens a history database in a temporary directory — never the
// user's real one.
func newHistoryDB(t *testing.T) *history.DB {
	t.Helper()
	db, err := history.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("opening test history: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// storedDemo builds a hand-made complete analysis with three named players.
func storedDemo(mapName, gameMode string, names map[uint64]string) *analysis.MapAnalysis {
	players := make(map[uint64]*analysis.DemoPlayer, len(names))
	for id, name := range names {
		players[id] = &analysis.DemoPlayer{
			SteamID: id,
			Name:    name,
			Deaths:  14,
			KillStats: analysis.KillStats{
				Total: 20, HeadShots: 9, TradeKills: 3, Precision: 0.45,
				WeaponsKills: map[string]int{"AK-47": 20},
			},
			DeathsTraded:   analysis.SideCount{Total: 5},
			AssistStats:    analysis.AssistStats{DamageGiven: 1900, ADR: 90.5},
			PlayerMapStats: analysis.PlayerMapStats{KAST: 76.2},
			OpeningDuelStats: analysis.OpeningDuelStats{
				OpeningKills:       analysis.SideCount{Total: 6},
				OpeningDeaths:      analysis.SideCount{Total: 2},
				OpeningSuccessRate: 66.7,
			},
			UtilityStats: analysis.UtilityStats{
				EnemiesFlashed: 4,
				UtilityDamage:  analysis.UtilityDamageStats{Total: 55},
				GrenadesThrown: analysis.GrenadesThrownStats{Total: 17},
			},
		}
	}
	return &analysis.MapAnalysis{
		Players:  players,
		GameMode: gameMode,
		Map:      analysis.MapData{MapName: mapName, TotalRounds: 21, RoundsWonCT: 8, RoundsWonT: 13},
		Teams: []analysis.DemoTeam{
			{TeamID: 1, Name: "One", Score: 13, Roster: []uint64{testSteamOne, testSteamTwo}},
			{TeamID: 2, Name: "Two", Score: 8, Roster: []uint64{testSteamThree}},
		},
	}
}

// storedFacts builds matching exact facts for every player of a demo.
func storedFacts(demo *analysis.MapAnalysis) map[uint64]analysis.PlayerAggregationFacts {
	facts := make(map[uint64]analysis.PlayerAggregationFacts, len(demo.Players))
	for id := range demo.Players {
		facts[id] = analysis.PlayerAggregationFacts{
			KASTRounds:       analysis.SideCount{Total: 16, CT: 8, T: 8},
			SideDamage:       analysis.SideCount{Total: 1900, CT: 950, T: 950},
			OpeningRoundsWon: 4,
		}
	}
	return facts
}

// storeTestMatch stores one hand-made match and returns its digest.
func storeTestMatch(t *testing.T, db *history.DB, digest byte, demo *analysis.MapAnalysis,
	analysedAt time.Time, selected []uint64) string {
	t.Helper()
	sha := strings.Repeat(string([]byte{'a' + digest%6}), 32) + strings.Repeat("0", 32)
	_, err := db.StoreMatch(context.Background(), history.StoreMatchInput{
		SHA256:           sha,
		AnalysedAt:       analysedAt,
		AnalysisVersion:  "vtest",
		Analysis:         demo,
		Facts:            storedFacts(demo),
		SelectedSteamIDs: selected,
	})
	if err != nil {
		t.Fatalf("storing test match: %v", err)
	}
	return sha
}

func defaultNames() map[uint64]string {
	return map[uint64]string{testSteamOne: "Alpha", testSteamTwo: "Bravo", testSteamThree: "Charlie"}
}

// TestRunHistoryListEmpty pins the empty history message and success.
func TestRunHistoryListEmpty(t *testing.T) {
	db := newHistoryDB(t)
	var out strings.Builder
	if err := runHistoryList(context.Background(), db, &out); err != nil {
		t.Fatalf("runHistoryList: %v", err)
	}
	const want = "No matches in history yet. Run 'cs2-analyser-tool analyse' and successful analyses are stored automatically.\n"
	if got := out.String(); got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

// TestRunHistoryListNewestFirst pins the listing columns and ordering: the
// newer analysis appears above the older one, with the 12-character ID, the
// local analysis time, map, score, and game mode.
func TestRunHistoryListNewestFirst(t *testing.T) {
	db := newHistoryDB(t)
	older := storeTestMatch(t, db, 0, storedDemo("de_ancient", "competitive", defaultNames()), testAnalysedAt, nil)
	newer := storeTestMatch(t, db, 1, storedDemo("de_mirage", "premier", defaultNames()), testAnalysedAt.Add(time.Hour), nil)

	var out strings.Builder
	if err := runHistoryList(context.Background(), db, &out); err != nil {
		t.Fatalf("runHistoryList: %v", err)
	}
	listing := out.String()
	newerAt := strings.Index(listing, newer[:12])
	olderAt := strings.Index(listing, older[:12])
	if newerAt == -1 || olderAt == -1 || newerAt > olderAt {
		t.Errorf("listing does not show the newer analysis first:\n%s", listing)
	}
	for _, want := range []string{"de_mirage", "de_ancient", "13:8", "premier", "competitive",
		formatAnalysedAt(testAnalysedAt)} {
		if !strings.Contains(listing, want) {
			t.Errorf("listing lacks %q:\n%s", want, listing)
		}
	}
}

// TestRunHistoryShowAppliesStoredPreference pins re-rendering from storage
// alone: the stored preference narrows the view to the selected player while
// the canonical stored match keeps everyone.
func TestRunHistoryShowAppliesStoredPreference(t *testing.T) {
	ctx := context.Background()
	db := newHistoryDB(t)
	digest := storeTestMatch(t, db, 0, storedDemo("de_mirage", "premier", defaultNames()),
		testAnalysedAt, []uint64{testSteamTwo})

	var out strings.Builder
	if err := runHistoryShow(ctx, db, &out, digest[:12], false); err != nil {
		t.Fatalf("runHistoryShow: %v", err)
	}
	rendered := out.String()
	if !strings.Contains(rendered, "Bravo") {
		t.Errorf("output lacks the selected player:\n%s", rendered)
	}
	for _, hidden := range []string{"Alpha", "Charlie"} {
		if strings.Contains(rendered, hidden) {
			t.Errorf("output shows %s despite the stored preference:\n%s", hidden, rendered)
		}
	}
	if !strings.Contains(rendered, "Showing 1 of 3 stored players") {
		t.Errorf("output lacks the preference note:\n%s", rendered)
	}
	// The table footer renders the map name uppercased.
	if !strings.Contains(rendered, "DE_MIRAGE") {
		t.Errorf("output lacks the map footer:\n%s", rendered)
	}

	// Filtering was a view: the canonical stored match still has everyone.
	match, err := db.MatchByID(ctx, digest)
	if err != nil {
		t.Fatalf("MatchByID: %v", err)
	}
	if len(match.Analysis.Players) != 3 {
		t.Errorf("canonical players = %d after filtered show, want 3", len(match.Analysis.Players))
	}
}

// TestRunHistoryShowWithoutPreferenceShowsEveryone pins the no-rows-means-
// everyone rule and the --details tables.
func TestRunHistoryShowWithoutPreferenceShowsEveryone(t *testing.T) {
	db := newHistoryDB(t)
	digest := storeTestMatch(t, db, 0, storedDemo("de_mirage", "premier", defaultNames()), testAnalysedAt, nil)

	var out strings.Builder
	if err := runHistoryShow(context.Background(), db, &out, digest, true); err != nil {
		t.Fatalf("runHistoryShow: %v", err)
	}
	rendered := out.String()
	for _, name := range []string{"Alpha", "Bravo", "Charlie"} {
		if !strings.Contains(rendered, name) {
			t.Errorf("output lacks player %s:\n%s", name, rendered)
		}
	}
	if !strings.Contains(rendered, "Rating breakdown") {
		t.Errorf("--details output lacks the detail tables:\n%s", rendered)
	}
}

// TestRunHistoryShowExplicitEmptySelection pins the series-substitution
// case: a stored selection that kept no player of this map renders the
// explicit empty view, not everyone.
func TestRunHistoryShowExplicitEmptySelection(t *testing.T) {
	db := newHistoryDB(t)
	digest := storeTestMatch(t, db, 0, storedDemo("de_mirage", "premier", defaultNames()),
		testAnalysedAt, []uint64{})

	var out strings.Builder
	if err := runHistoryShow(context.Background(), db, &out, digest, false); err != nil {
		t.Fatalf("runHistoryShow: %v", err)
	}
	rendered := out.String()
	if !strings.Contains(rendered, "Showing 0 of 3 stored players") {
		t.Errorf("output lacks the empty-selection note:\n%s", rendered)
	}
	for _, hidden := range []string{"Alpha", "Bravo", "Charlie"} {
		if strings.Contains(rendered, hidden) {
			t.Errorf("output shows %s despite the explicit empty selection:\n%s", hidden, rendered)
		}
	}
}

// TestRunHistoryShowRejectsBadIDs pins that invalid and unknown IDs surface
// as errors.
func TestRunHistoryShowRejectsBadIDs(t *testing.T) {
	db := newHistoryDB(t)
	var out strings.Builder
	if err := runHistoryShow(context.Background(), db, &out, "1234", false); err == nil {
		t.Error("a 4-character ID was accepted")
	}
	if err := runHistoryShow(context.Background(), db, &out, "deadbeef", false); err == nil {
		t.Error("an unknown ID was accepted")
	}
}

// TestStoreAnalysedMapsPersistsAndReportsDuplicates drives the post-analysis
// storage hook against an overridden history directory and pins both status
// lines and the stored row.
func TestStoreAnalysedMapsPersistsAndReportsDuplicates(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	t.Setenv(history.EnvDir, dir)
	demo := storedDemo("de_mirage", "premier", defaultNames())
	input := history.StoreMatchInput{
		SHA256:          strings.Repeat("b", 64),
		AnalysedAt:      testAnalysedAt,
		AnalysisVersion: "vtest",
		Analysis:        demo,
		Facts:           storedFacts(demo),
	}

	var out strings.Builder
	if err := storeAnalysedMaps(ctx, &out, []history.StoreMatchInput{input}); err != nil {
		t.Fatalf("storeAnalysedMaps: %v", err)
	}
	if !strings.Contains(out.String(), "stored match "+input.SHA256[:12]) {
		t.Errorf("output %q lacks the stored confirmation", out.String())
	}

	out.Reset()
	if err := storeAnalysedMaps(ctx, &out, []history.StoreMatchInput{input}); err != nil {
		t.Fatalf("duplicate storeAnalysedMaps: %v", err)
	}
	if !strings.Contains(out.String(), "already stored") {
		t.Errorf("output %q lacks the duplicate notice", out.String())
	}

	db, err := history.Open(ctx, dir)
	if err != nil {
		t.Fatalf("reopening: %v", err)
	}
	defer db.Close()
	matches, err := db.ListMatches(ctx)
	if err != nil || len(matches) != 1 {
		t.Fatalf("stored matches = %d, %v; want exactly 1", len(matches), err)
	}
}

// TestStoreAnalysedMapsFailureReturnsError pins the persistence-failure
// contract after a successful analysis: the helper fails with a clear error
// — the caller joins it with any export error — when the history location
// cannot be a directory.
func TestStoreAnalysedMapsFailureReturnsError(t *testing.T) {
	blocked := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocked, []byte("occupied"), 0o600); err != nil {
		t.Fatalf("planting file: %v", err)
	}
	t.Setenv(history.EnvDir, filepath.Join(blocked, "history"))

	demo := storedDemo("de_mirage", "premier", defaultNames())
	err := storeAnalysedMaps(context.Background(), &strings.Builder{}, []history.StoreMatchInput{{
		SHA256:          strings.Repeat("c", 64),
		AnalysedAt:      testAnalysedAt,
		AnalysisVersion: "vtest",
		Analysis:        demo,
		Facts:           storedFacts(demo),
	}})
	if err == nil || !strings.Contains(err.Error(), "storing analysis in history") {
		t.Fatalf("err = %v, want a history storage failure", err)
	}
}

// TestSeriesStoreInputsNarrowSelectionPerMap pins the series storage shape:
// one input per played map with its own digest and — per map — only the
// selected SteamIDs that actually appear in it. A map none of the selected
// players appeared in keeps a non-nil empty selection (the explicit empty
// view), while no selection at all stays nil (everyone). The series
// aggregate itself is never among the inputs.
func TestSeriesStoreInputsNarrowSelectionPerMap(t *testing.T) {
	mapOne := storedDemo("de_mirage", "premier", defaultNames())
	mapTwo := storedDemo("de_ancient", "premier", map[uint64]string{
		testSteamOne: "Alpha", testSteamThree: "Charlie",
	})
	mapThree := storedDemo("de_nuke", "premier", map[uint64]string{
		testSteamThree: "Charlie",
	})
	inputs := []analysis.SeriesMapInput{
		{Demo: mapOne, SHA256: strings.Repeat("1", 64)},
		{Demo: mapTwo, SHA256: strings.Repeat("2", 64)},
		{Demo: mapThree, SHA256: strings.Repeat("3", 64)},
	}
	selected := map[uint64]bool{testSteamOne: true, testSteamTwo: true}

	stores := seriesStoreInputs(inputs, selected, testAnalysedAt)
	if len(stores) != 3 {
		t.Fatalf("stores = %d, want one per played map", len(stores))
	}
	if got, want := stores[0].SelectedSteamIDs, []uint64{testSteamOne, testSteamTwo}; !slices.Equal(got, want) {
		t.Errorf("map one selection = %v, want %v", got, want)
	}
	if got, want := stores[1].SelectedSteamIDs, []uint64{testSteamOne}; !slices.Equal(got, want) {
		t.Errorf("map two selection = %v, want only the present player %v", got, want)
	}
	if got := stores[2].SelectedSteamIDs; got == nil || len(got) != 0 {
		t.Errorf("map three selection = %v, want a non-nil empty explicit selection", got)
	}
	for i, store := range stores {
		if store.SHA256 != inputs[i].SHA256 {
			t.Errorf("store %d digest does not follow its played map", i)
		}
		if store.Analysis != inputs[i].Demo {
			t.Errorf("store %d does not carry the complete unfiltered map analysis", i)
		}
		if !store.AnalysedAt.Equal(testAnalysedAt) {
			t.Errorf("store %d analysedAt = %v, want the shared run time", i, store.AnalysedAt)
		}
	}

	// Without --players there is no selection to store at all.
	for i, store := range seriesStoreInputs(inputs, nil, testAnalysedAt) {
		if store.SelectedSteamIDs != nil {
			t.Errorf("store %d selection = %v without --players, want nil (everyone)", i, store.SelectedSteamIDs)
		}
	}
}
