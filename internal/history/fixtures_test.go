package history

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/taua-almeida/cs2-analyser-tool/analysis"
)

// The fixture match has two players on team 1 and one on team 2, with stats
// chosen so every trend rate divides into awkward, easily hand-checked
// fractions.
const (
	fixturePlayerOne   uint64 = 76561198000000001
	fixturePlayerTwo   uint64 = 76561198000000002
	fixturePlayerThree uint64 = 76561198000000003
)

// fixtureBase is the injected analysis time every fixture stores unless a
// test overrides it — a fixed clock, never the wall clock.
var fixtureBase = time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC)

// testDigest builds a distinct, valid 64-character lowercase digest.
func testDigest(n int) string {
	return fmt.Sprintf("%064x", n)
}

// fixtureOptions tweaks one stored fixture match.
type fixtureOptions struct {
	gameMode   string    // default "premier"
	mapName    string    // default "de_mirage"
	analysedAt time.Time // default fixtureBase
	version    string    // default "v0.2.0-test"
	selected   []uint64  // display preference to store
	players    map[uint64]*analysis.DemoPlayer
	facts      map[uint64]analysis.PlayerAggregationFacts
	noTeams    bool // drop logical teams to exercise the side-score fallback
}

// fixtureDemo builds the complete unfiltered analysis of one fixture map.
func fixtureDemo(opts fixtureOptions) *analysis.MapAnalysis {
	gameMode := opts.gameMode
	if gameMode == "" {
		gameMode = "premier"
	}
	mapName := opts.mapName
	if mapName == "" {
		mapName = "de_mirage"
	}
	players := opts.players
	if players == nil {
		players = map[uint64]*analysis.DemoPlayer{
			fixturePlayerOne:   fixturePlayer(fixturePlayerOne, "Alpha", 1),
			fixturePlayerTwo:   fixturePlayer(fixturePlayerTwo, "Bravo", 1),
			fixturePlayerThree: fixturePlayer(fixturePlayerThree, "Charlie", 2),
		}
	}
	demo := &analysis.MapAnalysis{
		Players:  players,
		GameMode: gameMode,
		Map:      analysis.MapData{MapName: mapName, TotalRounds: 21, RoundsWonCT: 8, RoundsWonT: 13},
	}
	if !opts.noTeams {
		demo.Teams = []analysis.DemoTeam{
			{TeamID: 1, Name: "One", Score: 13, Roster: []uint64{fixturePlayerOne, fixturePlayerTwo}},
			{TeamID: 2, Name: "Two", Score: 8, Roster: []uint64{fixturePlayerThree}},
		}
	}
	return demo
}

// fixturePlayer builds one player whose additive stats are all nonzero and
// distinct, derived from the SteamID so different players differ.
func fixturePlayer(id uint64, name string, teamID int) *analysis.DemoPlayer {
	n := int(id % 10)
	return &analysis.DemoPlayer{
		SteamID:      id,
		Name:         name,
		TeamID:       teamID,
		Deaths:       10 + n,
		DeathsTraded: analysis.SideCount{Total: 3 + n, CT: 2, T: 1 + n},
		KillStats: analysis.KillStats{
			Total:        15 + n,
			HeadShots:    7 + n,
			TradeKills:   4 + n,
			WeaponsKills: map[string]int{"AK-47": 15 + n},
		},
		AssistStats:    analysis.AssistStats{Total: 5, DamageGiven: 1500 + 100*n, ADR: 71.4},
		PlayerMapStats: analysis.PlayerMapStats{KAST: 66.7},
		OpeningDuelStats: analysis.OpeningDuelStats{
			OpeningKills:  analysis.SideCount{Total: 4 + n, CT: 2, T: 2 + n},
			OpeningDeaths: analysis.SideCount{Total: 2 + n, CT: 1, T: 1 + n},
		},
		SideStats: analysis.SideStats{Rounds: analysis.SideCount{Total: 21, CT: 10, T: 11}},
		UtilityStats: analysis.UtilityStats{
			EnemiesFlashed:        6 + n,
			EnemyFlashTimeSeconds: 12.5,
			UtilityDamage:         analysis.UtilityDamageStats{Total: 90 + n, HE: 60, Fire: 30 + n},
			GrenadesThrown:        analysis.GrenadesThrownStats{Total: 20 + n, Flash: 8, Smoke: 6, HE: 4, Molotov: 2 + n},
			UnusedUtilityValue:    800 + 50*n,
		},
	}
}

// fixtureFacts builds exact aggregation facts for the given players.
func fixtureFacts(players map[uint64]*analysis.DemoPlayer) map[uint64]analysis.PlayerAggregationFacts {
	facts := make(map[uint64]analysis.PlayerAggregationFacts, len(players))
	for id := range players {
		n := int(id % 10)
		facts[id] = analysis.PlayerAggregationFacts{
			KASTRounds:       analysis.SideCount{Total: 14 + n, CT: 7, T: 7 + n},
			SideDamage:       analysis.SideCount{Total: 1500 + 100*n, CT: 800, T: 700 + 100*n},
			OpeningRoundsWon: 3 + n,
			EcoKills:         12.5,
			EcoDamage:        1400.25,
			EcoSurvival:      6.5,
			EcoRatingKAST:    15.75,
			RoundSwing:       -0.625,
		}
	}
	return facts
}

// fixtureStoreInput assembles a complete store input for one fixture match.
func fixtureStoreInput(digest string, opts fixtureOptions) StoreMatchInput {
	demo := fixtureDemo(opts)
	facts := opts.facts
	if facts == nil {
		facts = fixtureFacts(demo.Players)
	}
	analysedAt := opts.analysedAt
	if analysedAt.IsZero() {
		analysedAt = fixtureBase
	}
	version := opts.version
	if version == "" {
		version = "v0.2.0-test"
	}
	return StoreMatchInput{
		SHA256:           digest,
		AnalysedAt:       analysedAt,
		AnalysisVersion:  version,
		Analysis:         demo,
		Facts:            facts,
		SelectedSteamIDs: opts.selected,
	}
}

// storeFixtureMatch stores one fixture match and returns its digest.
func storeFixtureMatch(t *testing.T, db *DB, digest string, opts fixtureOptions) string {
	t.Helper()
	result, err := db.StoreMatch(context.Background(), fixtureStoreInput(digest, opts))
	if err != nil {
		t.Fatalf("storing fixture match %s: %v", digest, err)
	}
	if !result.Created {
		t.Fatalf("fixture match %s was not created", digest)
	}
	return digest
}

// openBare opens the history database file with plain driver defaults,
// bypassing Open entirely, for tests that plant or inspect raw state.
func openBare(dir string) (*sql.DB, error) {
	return sql.Open("sqlite", filepath.Join(dir, dbFileName))
}

// countRows counts a table's rows; table names come from test constants.
func countRows(t *testing.T, db *DB, table string) int {
	t.Helper()
	var count int
	if err := db.sql.QueryRow("SELECT count(*) FROM " + table).Scan(&count); err != nil {
		t.Fatalf("counting %s: %v", table, err)
	}
	return count
}
