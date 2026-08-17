package history

import (
	"context"
	"errors"
	"math"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/taua-almeida/cs2-analyser-tool/analysis"
)

// TestStorePersistsAcrossReopen stores one match, reopens the database file,
// and requires the complete canonical row, the players, the facts and the
// preference to come back exactly.
func TestStorePersistsAcrossReopen(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db := openTestDBAt(t, dir)
	digest := testDigest(1)
	storeFixtureMatch(t, db, digest, fixtureOptions{selected: []uint64{fixturePlayerTwo, fixturePlayerOne}})
	db.Close()

	reopened := openTestDBAt(t, dir)
	match, err := reopened.MatchByID(ctx, digest)
	if err != nil {
		t.Fatalf("MatchByID after reopen: %v", err)
	}
	if match.MapName != "de_mirage" || match.GameMode != "premier" || match.AnalysisVersion != "v0.2.0-test" {
		t.Errorf("summary = %+v, want the stored fixture values", match.MatchSummary)
	}
	if !match.AnalysedAt.Equal(fixtureBase) {
		t.Errorf("analysed_at = %v, want the injected clock %v", match.AnalysedAt, fixtureBase)
	}
	if match.ScoreKind != ScoreKindTeams || match.ScoreA != 13 || match.ScoreB != 8 {
		t.Errorf("score = %s %d:%d, want teams 13:8", match.ScoreKind, match.ScoreA, match.ScoreB)
	}
	if len(match.Analysis.Players) != 3 {
		t.Errorf("stored analysis has %d players, want the complete unfiltered 3", len(match.Analysis.Players))
	}
	if player := match.Analysis.Players[fixturePlayerThree]; player == nil || player.Name != "Charlie" {
		t.Errorf("player three = %+v, want the unfiltered stored player", player)
	}
	wantSelection := []uint64{fixturePlayerOne, fixturePlayerTwo}
	if !match.SelectionExplicit || !slices.Equal(match.SelectedSteamIDs, wantSelection) {
		t.Errorf("selection = explicit %t %v, want an explicit sorted %v",
			match.SelectionExplicit, match.SelectedSteamIDs, wantSelection)
	}

	trend, err := reopened.PremierTrend(ctx, fixturePlayerOne)
	if err != nil {
		t.Fatalf("PremierTrend after reopen: %v", err)
	}
	if len(trend.Matches) != 1 {
		t.Fatalf("trend has %d matches, want 1", len(trend.Matches))
	}
	wantFacts := fixtureFacts(fixtureDemo(fixtureOptions{}).Players)[fixturePlayerOne]
	if trend.Matches[0].Facts != wantFacts {
		t.Errorf("stored facts = %+v, want %+v", trend.Matches[0].Facts, wantFacts)
	}
}

// TestStoreSideScoreFallback pins score_kind for a map without two resolved
// logical teams: the final CT and T side scores are stored instead.
func TestStoreSideScoreFallback(t *testing.T) {
	db := openTestDB(t)
	digest := storeFixtureMatch(t, db, testDigest(1), fixtureOptions{noTeams: true})
	match, err := db.MatchByID(context.Background(), digest)
	if err != nil {
		t.Fatalf("MatchByID: %v", err)
	}
	if match.ScoreKind != ScoreKindSides || match.ScoreA != 8 || match.ScoreB != 13 {
		t.Errorf("score = %s %d:%d, want sides 8:13", match.ScoreKind, match.ScoreA, match.ScoreB)
	}
}

// TestStoreDuplicateKeepsCanonicalMatch pins immutability under
// deduplication: a second store of the same digest — even claiming a
// different analysis, version and time — changes neither the canonical row
// nor the players, and only replaces the display preference.
func TestStoreDuplicateKeepsCanonicalMatch(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	digest := testDigest(1)
	storeFixtureMatch(t, db, digest, fixtureOptions{selected: []uint64{fixturePlayerOne}})

	later := fixtureBase.Add(48 * time.Hour)
	result, err := db.StoreMatch(ctx, fixtureStoreInput(digest, fixtureOptions{
		mapName:    "de_should_not_replace",
		version:    "v9.9.9",
		analysedAt: later,
		selected:   []uint64{fixturePlayerThree},
	}))
	if err != nil {
		t.Fatalf("duplicate store: %v", err)
	}
	if result.Created {
		t.Fatal("duplicate store reported Created")
	}

	match, err := db.MatchByID(ctx, digest)
	if err != nil {
		t.Fatalf("MatchByID: %v", err)
	}
	if match.MapName != "de_mirage" || match.AnalysisVersion != "v0.2.0-test" {
		t.Errorf("canonical row changed: %+v", match.MatchSummary)
	}
	if !match.AnalysedAt.Equal(fixtureBase) {
		t.Errorf("analysed_at = %v, want the original %v", match.AnalysedAt, fixtureBase)
	}
	if !slices.Equal(match.SelectedSteamIDs, []uint64{fixturePlayerThree}) {
		t.Errorf("selection = %v, want the replaced preference [%d]", match.SelectedSteamIDs, fixturePlayerThree)
	}
	if got := countRows(t, db, "matches"); got != 1 {
		t.Errorf("matches rows = %d, want 1", got)
	}
}

// TestStoreDuplicateCanClearPreference pins that re-analysing without a
// selection stores the display-everyone state: no selection, no preference
// rows.
func TestStoreDuplicateCanClearPreference(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	digest := testDigest(1)
	storeFixtureMatch(t, db, digest, fixtureOptions{selected: []uint64{fixturePlayerOne}})

	if _, err := db.StoreMatch(ctx, fixtureStoreInput(digest, fixtureOptions{})); err != nil {
		t.Fatalf("duplicate store: %v", err)
	}
	match, err := db.MatchByID(ctx, digest)
	if err != nil {
		t.Fatalf("MatchByID: %v", err)
	}
	if match.SelectionExplicit || len(match.SelectedSteamIDs) != 0 {
		t.Errorf("selection = explicit %t %v, want the everyone state",
			match.SelectionExplicit, match.SelectedSteamIDs)
	}
}

// TestStoreExplicitEmptySelection pins the empty-view case: an explicit
// selection whose players all sat this map out — a non-nil, empty (or fully
// absent) ID list — stays an explicit selection of nobody rather than
// falling back to everyone.
func TestStoreExplicitEmptySelection(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	const absent = uint64(76561198999999999)
	input := fixtureStoreInput(testDigest(1), fixtureOptions{})
	input.SelectedSteamIDs = []uint64{absent}
	if _, err := db.StoreMatch(ctx, input); err != nil {
		t.Fatalf("StoreMatch: %v", err)
	}

	match, err := db.MatchByID(ctx, testDigest(1))
	if err != nil {
		t.Fatalf("MatchByID: %v", err)
	}
	if !match.SelectionExplicit || len(match.SelectedSteamIDs) != 0 {
		t.Errorf("selection = explicit %t %v, want an explicit empty selection",
			match.SelectionExplicit, match.SelectedSteamIDs)
	}
}

// TestStoreDistinctContentIsDistinctMatches pins that deduplication is by
// content digest alone: two different digests stay two matches even with
// identical map names and analysis payloads.
func TestStoreDistinctContentIsDistinctMatches(t *testing.T) {
	db := openTestDB(t)
	storeFixtureMatch(t, db, testDigest(1), fixtureOptions{})
	storeFixtureMatch(t, db, testDigest(2), fixtureOptions{})
	if got := countRows(t, db, "matches"); got != 2 {
		t.Errorf("matches rows = %d, want 2 distinct matches", got)
	}
}

// TestStoreSelectionIsNarrowedToPresentPlayers pins the preference rules: a
// selection is sorted, deduplicated, and silently narrowed to the players
// stored for the map — a series-wide selection legitimately names players
// who sat one map out.
func TestStoreSelectionIsNarrowedToPresentPlayers(t *testing.T) {
	db := openTestDB(t)
	const absent = uint64(76561198999999999)
	digest := storeFixtureMatch(t, db, testDigest(1), fixtureOptions{
		selected: []uint64{fixturePlayerTwo, absent, fixturePlayerTwo, fixturePlayerOne},
	})
	match, err := db.MatchByID(context.Background(), digest)
	if err != nil {
		t.Fatalf("MatchByID: %v", err)
	}
	want := []uint64{fixturePlayerOne, fixturePlayerTwo}
	if !slices.Equal(match.SelectedSteamIDs, want) {
		t.Errorf("selection = %v, want %v", match.SelectedSteamIDs, want)
	}
}

// TestStoreInputValidation pins the up-front rejections: malformed digests,
// facts not matching the players, and a zero SteamID in the selection.
func TestStoreInputValidation(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	for _, digest := range []string{"", "abc", strings.Repeat("A", 64), strings.Repeat("g", 64), testDigest(1) + "aa"} {
		if _, err := db.StoreMatch(ctx, fixtureStoreInput(digest, fixtureOptions{})); err == nil {
			t.Errorf("digest %q was accepted", digest)
		}
	}

	missingFacts := fixtureStoreInput(testDigest(1), fixtureOptions{})
	delete(missingFacts.Facts, fixturePlayerTwo)
	if _, err := db.StoreMatch(ctx, missingFacts); err == nil ||
		!strings.Contains(err.Error(), "players without facts") {
		t.Errorf("missing facts error = %v, want the mismatch rejection", err)
	}

	extraFacts := fixtureStoreInput(testDigest(1), fixtureOptions{})
	extraFacts.Facts[42424242] = analysis.PlayerAggregationFacts{}
	if _, err := db.StoreMatch(ctx, extraFacts); err == nil ||
		!strings.Contains(err.Error(), "facts without players") {
		t.Errorf("extra facts error = %v, want the mismatch rejection", err)
	}

	zeroSelection := fixtureStoreInput(testDigest(1), fixtureOptions{})
	zeroSelection.SelectedSteamIDs = []uint64{0}
	if _, err := db.StoreMatch(ctx, zeroSelection); err == nil ||
		!strings.Contains(err.Error(), "SteamID 0") {
		t.Errorf("zero selection error = %v, want rejection", err)
	}

	if got := countRows(t, db, "matches"); got != 0 {
		t.Errorf("matches rows = %d after rejected stores, want 0", got)
	}
}

// invalidPlayersFixture returns a store input whose players pass the
// up-front checks but fail inside the transaction, after the parent match
// row was inserted.
func invalidPlayersFixture(digest string, corrupt func(map[uint64]*analysis.DemoPlayer)) StoreMatchInput {
	input := fixtureStoreInput(digest, fixtureOptions{})
	corrupt(input.Analysis.Players)
	input.Facts = fixtureFacts(input.Analysis.Players)
	return input
}

// TestStoreFailedPlayerInsertRollsBackWholeMatch pins transactionality: when
// a player row fails mid-transaction, the new match vanishes completely and
// previously stored history is untouched. Both failure shapes are exercised
// — an invalid SteamID and a player value JSON cannot encode (NaN), the
// latter pinning that invalid JSON fails before commit.
func TestStoreFailedPlayerInsertRollsBackWholeMatch(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	survivor := storeFixtureMatch(t, db, testDigest(7), fixtureOptions{selected: []uint64{fixturePlayerOne}})

	failures := map[string]StoreMatchInput{
		"zero steam id": invalidPlayersFixture(testDigest(1), func(players map[uint64]*analysis.DemoPlayer) {
			players[0] = fixturePlayer(0, "Ghost", 1)
		}),
		"key and player disagree": invalidPlayersFixture(testDigest(2), func(players map[uint64]*analysis.DemoPlayer) {
			players[fixturePlayerOne].SteamID = fixturePlayerTwo
		}),
		"unencodable player": invalidPlayersFixture(testDigest(3), func(players map[uint64]*analysis.DemoPlayer) {
			players[fixturePlayerOne].Rating.Value = math.NaN()
		}),
	}
	for name, input := range failures {
		if _, err := db.StoreMatch(ctx, input); err == nil {
			t.Fatalf("%s: store succeeded, want rollback", name)
		}
		if _, err := db.MatchByID(ctx, input.SHA256); err == nil {
			t.Errorf("%s: the failed match exists", name)
		}
	}

	if got := countRows(t, db, "matches"); got != 1 {
		t.Errorf("matches rows = %d, want only the survivor", got)
	}
	if got := countRows(t, db, "match_players"); got != 3 {
		t.Errorf("match_players rows = %d, want the survivor's 3", got)
	}
	match, err := db.MatchByID(ctx, survivor)
	if err != nil {
		t.Fatalf("survivor lookup: %v", err)
	}
	if !slices.Equal(match.SelectedSteamIDs, []uint64{fixturePlayerOne}) {
		t.Errorf("survivor selection = %v, want untouched [%d]", match.SelectedSteamIDs, fixturePlayerOne)
	}
}

// TestConcurrentDuplicateStoresKeepOneMatch races two handles storing the
// same digest and requires exactly one row, exactly one Created result, and
// no error from either.
func TestConcurrentDuplicateStoresKeepOneMatch(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	handles := [2]*DB{openTestDBAt(t, dir), openTestDBAt(t, dir)}

	var wg sync.WaitGroup
	results := make([]StoreResult, len(handles))
	errs := make([]error, len(handles))
	for i, handle := range handles {
		wg.Go(func() {
			results[i], errs[i] = handle.StoreMatch(ctx, fixtureStoreInput(testDigest(1), fixtureOptions{}))
		})
	}
	wg.Wait()

	if err := errors.Join(errs[:]...); err != nil {
		t.Fatalf("concurrent stores failed: %v", err)
	}
	created := 0
	for _, result := range results {
		if result.Created {
			created++
		}
	}
	if created != 1 {
		t.Errorf("%d stores reported Created, want exactly 1", created)
	}
	if got := countRows(t, handles[0], "matches"); got != 1 {
		t.Errorf("matches rows = %d, want 1", got)
	}
	if got := countRows(t, handles[0], "match_players"); got != 3 {
		t.Errorf("match_players rows = %d, want 3", got)
	}
}
