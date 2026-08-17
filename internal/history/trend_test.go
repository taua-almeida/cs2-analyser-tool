package history

import (
	"context"
	"errors"
	"math"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/taua-almeida/cs2-analyser-tool/analysis"
)

// namedPlayers builds the default fixture roster with player one renamed.
func namedPlayers(name string) map[uint64]*analysis.DemoPlayer {
	return map[uint64]*analysis.DemoPlayer{
		fixturePlayerOne:   fixturePlayer(fixturePlayerOne, name, 1),
		fixturePlayerTwo:   fixturePlayer(fixturePlayerTwo, "Bravo", 1),
		fixturePlayerThree: fixturePlayer(fixturePlayerThree, "Charlie", 2),
	}
}

// TestResolvePlayerParsesSteamIDFirst pins the identity order: a nonzero
// decimal SteamID64 is taken literally — stored or not — while zero falls
// through to alias resolution and fails like any unknown alias.
func TestResolvePlayerParsesSteamIDFirst(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	id, err := db.ResolvePlayer(ctx, "76561198000000042")
	if err != nil || id != 76561198000000042 {
		t.Fatalf("ResolvePlayer(steam id) = %d, %v; want the literal ID", id, err)
	}
	if _, err := db.ResolvePlayer(ctx, "0"); err == nil || !strings.Contains(err.Error(), "no stored player") {
		t.Errorf("ResolvePlayer(0) = %v, want the unknown-alias failure", err)
	}
	if _, err := db.ResolvePlayer(ctx, "Alpha"); err == nil || !strings.Contains(err.Error(), "no stored player") {
		t.Errorf("ResolvePlayer on empty history = %v, want the unknown-alias failure", err)
	}
}

// TestResolvePlayerFoldsUnicodeAliases pins Unicode-aware, case-insensitive
// alias resolution accumulated across map rows: SQLite NOCASE would fold
// neither Ö nor Cyrillic И, strings.EqualFold folds both.
func TestResolvePlayerFoldsUnicodeAliases(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	storeFixtureMatch(t, db, testDigest(1), fixtureOptions{players: namedPlayers("ÖZGÜR")})
	storeFixtureMatch(t, db, testDigest(2), fixtureOptions{players: namedPlayers("Игрок")})

	for _, alias := range []string{"özgür", "ÖZGÜR", "игрок", "ИГРОК"} {
		id, err := db.ResolvePlayer(ctx, alias)
		if err != nil {
			t.Errorf("ResolvePlayer(%q): %v", alias, err)
			continue
		}
		if id != fixturePlayerOne {
			t.Errorf("ResolvePlayer(%q) = %d, want %d", alias, id, fixturePlayerOne)
		}
	}
}

// TestResolvePlayerSimpleFoldingBoundary pins the documented limit of the
// contract's strings.EqualFold semantics: simple folding matches one-to-one
// case pairs only, so a multi-rune expansion like ß→ss does not match and
// the stored spelling itself still does.
func TestResolvePlayerSimpleFoldingBoundary(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	storeFixtureMatch(t, db, testDigest(1), fixtureOptions{players: namedPlayers("Straße")})

	if id, err := db.ResolvePlayer(ctx, "STRAßE"); err != nil || id != fixturePlayerOne {
		t.Errorf("ResolvePlayer(STRAßE) = %d, %v; want the case-folded match", id, err)
	}
	if _, err := db.ResolvePlayer(ctx, "STRASSE"); err == nil ||
		!strings.Contains(err.Error(), "no stored player") {
		t.Errorf("ResolvePlayer(STRASSE) = %v; simple folding must not expand ß to ss", err)
	}
}

// TestResolvePlayerAmbiguousAlias pins the structured failure when one alias
// names two stored SteamIDs: both candidates appear with their known
// aliases.
func TestResolvePlayerAmbiguousAlias(t *testing.T) {
	db := openTestDB(t)
	players := namedPlayers("Smurf")
	players[fixturePlayerTwo].Name = "smurf"
	storeFixtureMatch(t, db, testDigest(1), fixtureOptions{players: players})
	storeFixtureMatch(t, db, testDigest(2), fixtureOptions{players: namedPlayers("MainAccount")})

	_, err := db.ResolvePlayer(context.Background(), "SMURF")
	var ambiguous *AliasAmbiguityError
	if !errors.As(err, &ambiguous) {
		t.Fatalf("err = %v, want an AliasAmbiguityError", err)
	}
	if ambiguous.Alias != "SMURF" || len(ambiguous.Candidates) != 2 {
		t.Fatalf("ambiguity = %+v, want two candidates", ambiguous)
	}
	first := ambiguous.Candidates[0]
	if first.SteamID != fixturePlayerOne || !slices.Equal(first.Aliases, []string{"MainAccount", "Smurf"}) {
		t.Errorf("first candidate = %+v, want %d with aliases accumulated across maps", first, fixturePlayerOne)
	}
	if second := ambiguous.Candidates[1]; second.SteamID != fixturePlayerTwo || !slices.Equal(second.Aliases, []string{"Bravo", "smurf"}) {
		t.Errorf("second candidate = %+v, want %d with both its aliases", second, fixturePlayerTwo)
	}
}

// TestPremierTrendExactAdditiveTotals stores two premier maps, one
// competitive map, and one premier map without the player, then requires the
// trend to include exactly the two premier appearances in chronological
// order and every aggregate to be recomputed additively — never by
// averaging displayed rates.
func TestPremierTrendExactAdditiveTotals(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	earlier := fixtureBase.Add(-24 * time.Hour)
	// Stored newest first to prove the trend sorts chronologically rather
	// than by insertion.
	storeFixtureMatch(t, db, testDigest(1), fixtureOptions{players: namedPlayers("NewAlias")})
	storeFixtureMatch(t, db, testDigest(2), fixtureOptions{
		analysedAt: earlier, mapName: "de_ancient", players: namedPlayers("OldAlias"),
	})
	storeFixtureMatch(t, db, testDigest(3), fixtureOptions{gameMode: "competitive"})
	storeFixtureMatch(t, db, testDigest(4), fixtureOptions{players: map[uint64]*analysis.DemoPlayer{
		fixturePlayerThree: fixturePlayer(fixturePlayerThree, "Charlie", 2),
	}})

	trend, err := db.PremierTrend(ctx, fixturePlayerOne)
	if err != nil {
		t.Fatalf("PremierTrend: %v", err)
	}
	if trend.SteamID != fixturePlayerOne || len(trend.Matches) != 2 {
		t.Fatalf("trend = %d matches for %d, want the 2 premier appearances", len(trend.Matches), trend.SteamID)
	}
	if trend.Matches[0].MapName != "de_ancient" || trend.Matches[0].Alias != "OldAlias" {
		t.Errorf("first trend row = %s as %q, want the chronologically earlier de_ancient as OldAlias",
			trend.Matches[0].MapName, trend.Matches[0].Alias)
	}
	if trend.Matches[1].Alias != "NewAlias" || trend.Matches[1].Rounds != 21 {
		t.Errorf("second trend row = %q over %d rounds, want NewAlias over 21", trend.Matches[1].Alias, trend.Matches[1].Rounds)
	}
	if !slices.Equal(trend.Aliases, []string{"OldAlias", "NewAlias"}) {
		t.Errorf("aliases = %v, want first-observation order [OldAlias NewAlias]", trend.Aliases)
	}

	// Every total is the exact sum of the two fixture maps' values for
	// player one (n = 1 in the fixture arithmetic).
	want := TrendTotals{
		Maps: 2, Rounds: 42,
		Kills: 32, Deaths: 22, Headshots: 16, DamageGiven: 3200,
		KASTRounds: 30, OpeningRoundsWon: 8,
		OpeningKills: 10, OpeningDeaths: 6,
		TradeKills: 10, DeathsTraded: 8,
		UtilityDamage: 182, EnemiesFlashed: 14, EnemyFlashTimeSeconds: 25,
		GrenadesThrown: 42, UnusedUtilityValue: 1700,
	}
	if trend.Totals != want {
		t.Fatalf("totals = %+v, want %+v", trend.Totals, want)
	}
	rates := map[string][2]float64{
		"KD":              {trend.Totals.KD(), 32.0 / 22.0},
		"ADR":             {trend.Totals.ADR(), 3200.0 / 42.0},
		"KAST":            {trend.Totals.KASTPercent(), 100 * 30.0 / 42.0},
		"HS":              {trend.Totals.HSPercent(), 50},
		"Opening success": {trend.Totals.OpeningSuccessPercent(), 80},
	}
	for name, pair := range rates {
		if pair[0] != pair[1] {
			t.Errorf("%s = %v, want the exact quotient %v", name, pair[0], pair[1])
		}
	}
}

// TestPremierTrendEmptyAndInvalid pins that an unknown SteamID yields an
// empty trend without error while SteamID zero is rejected.
func TestPremierTrendEmptyAndInvalid(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	trend, err := db.PremierTrend(ctx, 76561198000000042)
	if err != nil {
		t.Fatalf("PremierTrend: %v", err)
	}
	if len(trend.Matches) != 0 || trend.Totals.Maps != 0 {
		t.Errorf("trend = %+v, want empty", trend)
	}
	if _, err := db.PremierTrend(ctx, 0); err == nil {
		t.Error("PremierTrend(0) succeeded, want rejection")
	}
}

// TestTrendTotalsZeroDenominators pins every zero-denominator rule the
// documented formulas define.
func TestTrendTotalsZeroDenominators(t *testing.T) {
	empty := TrendTotals{}
	for name, got := range map[string]float64{
		"KD":              empty.KD(),
		"ADR":             empty.ADR(),
		"KAST":            empty.KASTPercent(),
		"HS":              empty.HSPercent(),
		"Opening success": empty.OpeningSuccessPercent(),
	} {
		if got != 0 || math.IsNaN(got) {
			t.Errorf("empty %s = %v, want 0", name, got)
		}
	}
	deathless := TrendTotals{Kills: 7}
	if kd := deathless.KD(); kd != 7 {
		t.Errorf("deathless KD = %v, want the kill count 7 (divide by one)", kd)
	}
	roundless := TrendTotals{DamageGiven: 500, KASTRounds: 3}
	if roundless.ADR() != 0 || roundless.KASTPercent() != 0 {
		t.Errorf("roundless ADR/KAST = %v/%v, want 0/0", roundless.ADR(), roundless.KASTPercent())
	}
}
