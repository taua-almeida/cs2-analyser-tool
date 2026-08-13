package demoparser

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
)

const (
	hltvDemoDirEnv          = "HLTV_DEMO_DIR"
	requireHLTVDemosEnv     = "REQUIRE_HLTV_DEMOS"
	hltvExtraDemoDirsEnv    = "HLTV_EXTRA_DEMO_DIRS"
	requireHLTVExtraDemoEnv = "REQUIRE_HLTV_EXTRA_DEMOS"
	hltvOracleFixturePath   = "testdata/hltv-129241/expected.json"
	adrMetric               = "ADR"
	kastMetric              = "KAST qualifying rounds"
)

type hltvOracle struct {
	FixtureID     string            `json:"fixture_id"`
	MatchID       int               `json:"match_id"`
	MatchStatsURL string            `json:"match_stats_url"`
	Maps          []hltvMapExpected `json:"maps"`
}

type hltvMapExpected struct {
	Name          string               `json:"name"`
	MapID         int                  `json:"map_id"`
	MapStatsURL   string               `json:"map_stats_url"`
	DemoFile      string               `json:"demo_file"`
	DemoSHA256    string               `json:"demo_sha256"`
	ParserMapName string               `json:"parser_map_name"`
	GameMode      *string              `json:"game_mode"`
	Rounds        int                  `json:"rounds"`
	ScoreValues   [2]int               `json:"score_values"`
	Teams         *[2]hltvTeamExpected `json:"teams"`
	Players       []hltvPlayerExpected `json:"players"`
}

// hltvTeamExpected pins one logical team of a map: the clan name the
// tournament server showed on the scoreboard (audited against the HLTV team
// pages), the rounds that lineup won, and its five SteamID64s. Order within
// the fixture array is presentational; the parser's teams are matched to
// these rows by exact roster.
type hltvTeamExpected struct {
	Name     string   `json:"name"`
	Score    int      `json:"score"`
	SteamIDs []uint64 `json:"steam_ids"`
}

type hltvPlayerExpected struct {
	SteamID     uint64  `json:"steam_id,string"`
	HLTVName    string  `json:"hltv_name"`
	Kills       int     `json:"kills"`
	Deaths      int     `json:"deaths"`
	ADR         float64 `json:"adr"`
	KASTPercent float64 `json:"kast_percent"`
	Rating      float64 `json:"rating"`
}

type hltvDifferenceKey struct {
	FixtureID string
	MapID     int
	SteamID   uint64
	Metric    string
}

type hltvFixtureMapKey struct {
	FixtureID string
	MapID     int
}

type hltvExpectedDifference struct {
	FixtureID string
	MapID     int
	SteamID   uint64
	Metric    string
	HLTVValue string
	ToolValue string
	FollowUp  string
}

func (d hltvExpectedDifference) key() hltvDifferenceKey {
	return hltvDifferenceKey{
		FixtureID: d.FixtureID,
		MapID:     d.MapID,
		SteamID:   d.SteamID,
		Metric:    d.Metric,
	}
}

// These are known parser differences, not tolerances. Each row pins both
// sides of the current discrepancy: a new value fails, and reaching parity
// also fails until the stale exception is removed.
var hltvExpectedDifferences = []hltvExpectedDifference{
	{
		FixtureID: "match-129241", MapID: 234944, SteamID: 76561199048086137,
		Metric: kastMetric, HLTVValue: "18/22 (81.8%)", ToolValue: "17/22 (77.3%)", FollowUp: "issue #38",
	},
	{
		FixtureID: "match-128974", MapID: 234227, SteamID: 76561198081484775,
		Metric: adrMetric, HLTVValue: "49.6", ToolValue: "49.5", FollowUp: "ADR rounding follow-up",
	},
	{
		FixtureID: "match-128974", MapID: 234227, SteamID: 76561198310561479,
		Metric: adrMetric, HLTVValue: "108.7", ToolValue: "108.8", FollowUp: "ADR rounding follow-up",
	},
	{
		FixtureID: "match-128974", MapID: 234233, SteamID: 76561198081484775,
		Metric: adrMetric, HLTVValue: "59.6", ToolValue: "59.7", FollowUp: "ADR rounding follow-up",
	},
	{
		FixtureID: "match-128974", MapID: 234238, SteamID: 76561198998266210,
		Metric: adrMetric, HLTVValue: "64.8", ToolValue: "64.9", FollowUp: "ADR rounding follow-up",
	},
	{
		FixtureID: "match-128974", MapID: 234256, SteamID: 76561198193174134,
		Metric: adrMetric, HLTVValue: "81.4", ToolValue: "81.5", FollowUp: "ADR rounding follow-up",
	},
	{
		FixtureID: "match-128974", MapID: 234256, SteamID: 76561198310561479,
		Metric: adrMetric, HLTVValue: "76.5", ToolValue: "76.6", FollowUp: "ADR rounding follow-up",
	},
	{
		FixtureID: "match-2396559", MapID: 234956, SteamID: 76561199024583803,
		Metric: adrMetric, HLTVValue: "63.9", ToolValue: "64.0", FollowUp: "ADR rounding follow-up",
	},
	{
		FixtureID: "match-2396559", MapID: 234956, SteamID: 76561199063238565,
		Metric: kastMetric, HLTVValue: "18/23 (78.3%)", ToolValue: "17/23 (73.9%)", FollowUp: "issue #38",
	},
}

type hltvOracleSpec struct {
	Path         string
	ExpectedMaps int
	Extra        bool
}

var hltvOracleSpecs = []hltvOracleSpec{
	{Path: hltvOracleFixturePath, ExpectedMaps: 3},
	{Path: "testdata/hltv-128974/expected.json", ExpectedMaps: 4, Extra: true},
	{Path: "testdata/hltv-2396559/expected.json", ExpectedMaps: 1, Extra: true},
}

type ratingObservation struct {
	Tool float64
	HLTV float64
}

type ratingQuality struct {
	MAE       float64
	RMSE      float64
	Bias      float64
	Spearman  float64
	Within005 int
	Within010 int
	Within020 int
}

type hltvParity struct {
	Kills  int
	Deaths int
	ADR    int
	KAST   int
}

func TestHLTVRegression(t *testing.T) {
	oracles := make([]hltvOracle, len(hltvOracleSpecs))
	for i, spec := range hltvOracleSpecs {
		oracles[i] = loadHLTVOracle(t, spec.Path)
	}
	validateHLTVOracleSet(t, hltvOracleSpecs, oracles)
	validateHLTVExpectedDifferences(t, oracles)

	originalDirs := configuredDemoDirs(os.Getenv(hltvDemoDirEnv))
	extraDirs := configuredDemoDirs(os.Getenv(hltvExtraDemoDirsEnv))
	requireOriginal := os.Getenv(requireHLTVDemosEnv) != ""
	requireExtra := os.Getenv(requireHLTVExtraDemoEnv) != ""
	if requireOriginal && len(originalDirs) == 0 {
		t.Fatalf("%s is set but %s is empty; point it at the directory containing the three match 129241 demos",
			requireHLTVDemosEnv, hltvDemoDirEnv)
	}
	if requireExtra && len(extraDirs) == 0 {
		t.Fatalf("%s is set but %s is empty; provide an OS path-list containing the five additional demos",
			requireHLTVExtraDemoEnv, hltvExtraDemoDirsEnv)
	}

	var originalRatings, extraRatings []ratingObservation
	var originalParity, extraParity hltvParity
	for i, spec := range hltvOracleSpecs {
		oracle := oracles[i]
		dirs := originalDirs
		required := requireOriginal
		requireEnv := requireHLTVDemosEnv
		if spec.Extra {
			dirs = extraDirs
			required = requireExtra
			requireEnv = requireHLTVExtraDemoEnv
		}

		for _, expectedMap := range oracle.Maps {
			testName := expectedMap.Name
			if spec.Extra {
				testName = fmt.Sprintf("extra/%s/%s-%d", oracle.FixtureID, expectedMap.Name, expectedMap.MapID)
			}
			t.Run(testName, func(t *testing.T) {
				if len(dirs) == 0 {
					t.Skipf("HLTV demos are not configured; set %s to run this external-oracle fixture", demoDirsEnv(spec.Extra))
				}
				demoPath, err := findDemo(dirs, expectedMap.DemoFile)
				if err != nil {
					if errors.Is(err, os.ErrNotExist) && !required {
						t.Skipf("HLTV demo %s is absent; set %s to make missing demos fail", expectedMap.DemoFile, requireEnv)
					}
					t.Fatalf("locating HLTV demo: %v", err)
				}
				result := parseVerifiedHLTVDemo(t, demoPath, expectedMap.DemoSHA256)

				assertHLTVMap(t, oracle.FixtureID, result, expectedMap)
				assertHLTVRoster(t, oracle.FixtureID, result.Players, expectedMap)
				assertHLTVTeams(t, oracle.FixtureID, result, expectedMap)
				for _, expectedPlayer := range expectedMap.Players {
					player := result.Players[expectedPlayer.SteamID]
					assertHLTVPlayer(t, oracle.FixtureID, expectedMap, expectedPlayer, player)
					parity := &originalParity
					ratings := &originalRatings
					if spec.Extra {
						parity = &extraParity
						ratings = &extraRatings
					}
					observeHLTVParity(parity, player, expectedMap, expectedPlayer)
					*ratings = append(*ratings, ratingObservation{
						Tool: displayedRating(player.Rating.Value),
						HLTV: expectedPlayer.Rating,
					})
				}
			})
		}
	}

	logHLTVParity(t, "original fixture", originalParity, originalRatings)
	logHLTVParity(t, "additional fixtures", extraParity, extraRatings)
}

// TestHLTVSeriesRegression drives the whole BO3 pipeline over the original
// Rooster–Mindfreak fixture: every map is hashed and parsed in oracle order
// and aggregated with BuildSeries, and the result is checked against values
// derived from the same audited oracle — per-team map and round wins, the
// series winner, exact SteamID matching across all three maps, additive
// totals equal to the sum of the standalone map analyses, and the 64-round
// series denominator behind every rate. Like TestHLTVRegression it never
// downloads anything; unconfigured demos skip unless REQUIRE_HLTV_DEMOS is
// set.
func TestHLTVSeriesRegression(t *testing.T) {
	oracle := loadHLTVOracle(t, hltvOracleFixturePath)
	dirs := configuredDemoDirs(os.Getenv(hltvDemoDirEnv))
	required := os.Getenv(requireHLTVDemosEnv) != ""
	if required && len(dirs) == 0 {
		t.Fatalf("%s is set but %s is empty; point it at the directory containing the three match 129241 demos",
			requireHLTVDemosEnv, hltvDemoDirEnv)
	}
	if len(dirs) == 0 {
		t.Skipf("HLTV demos are not configured; set %s to run the series fixture", hltvDemoDirEnv)
	}

	inputs := make([]SeriesMapInput, 0, len(oracle.Maps))
	for _, expectedMap := range oracle.Maps {
		demoPath, err := findDemo(dirs, expectedMap.DemoFile)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) && !required {
				t.Skipf("HLTV demo %s is absent; set %s to make missing demos fail", expectedMap.DemoFile, requireHLTVDemosEnv)
			}
			t.Fatalf("locating HLTV demo: %v", err)
		}
		result := parseVerifiedHLTVDemo(t, demoPath, expectedMap.DemoSHA256)
		inputs = append(inputs, SeriesMapInput{Demo: result, SHA256: expectedMap.DemoSHA256})
	}

	series, err := BuildSeries(3, inputs)
	if err != nil {
		t.Fatalf("BuildSeries: %v", err)
	}
	if series.BestOf != 3 {
		t.Errorf("best_of = %d, want 3", series.BestOf)
	}
	assertHLTVSeriesMapsInOrder(t, series, oracle)
	assertHLTVSeriesTeams(t, series, oracle)
	assertHLTVSeriesPlayers(t, series, inputs, oracle)
}

// hltvSeriesTeamExpectation is one fixture team folded across the oracle's
// maps: its roster and its map and round wins.
type hltvSeriesTeamExpectation struct {
	name      string
	steamIDs  []uint64
	mapsWon   int
	roundsWon int
}

// hltvSeriesExpectations derives each fixture team's expected series result
// from the audited per-map oracle rows, so the test asserts oracle-derived
// values rather than numbers of its own.
func hltvSeriesExpectations(t *testing.T, oracle hltvOracle) []hltvSeriesTeamExpectation {
	t.Helper()
	var expectations []hltvSeriesTeamExpectation
	for _, expectedMap := range oracle.Maps {
		teams := *expectedMap.Teams
		for i, team := range teams {
			ids := sortedIDs(team.SteamIDs)
			index := slices.IndexFunc(expectations, func(e hltvSeriesTeamExpectation) bool {
				return e.name == team.Name
			})
			if index < 0 {
				index = len(expectations)
				expectations = append(expectations, hltvSeriesTeamExpectation{name: team.Name, steamIDs: ids})
			}
			expectation := &expectations[index]
			if !slices.Equal(expectation.steamIDs, ids) {
				t.Fatalf("oracle team %s changes roster between maps; the series fixture assumes stable lineups", team.Name)
			}
			expectation.roundsWon += team.Score
			if team.Score > teams[1-i].Score {
				expectation.mapsWon++
			}
		}
	}
	if len(expectations) != 2 {
		t.Fatalf("oracle names %d series teams, want 2", len(expectations))
	}
	return expectations
}

func assertHLTVSeriesMapsInOrder(t *testing.T, series *ProcessedSeries, oracle hltvOracle) {
	t.Helper()
	if len(series.Maps) != len(oracle.Maps) {
		t.Fatalf("series has %d maps, want %d", len(series.Maps), len(oracle.Maps))
	}
	for i, expectedMap := range oracle.Maps {
		if got := series.Maps[i].SHA256; got != expectedMap.DemoSHA256 {
			t.Errorf("maps[%d].sha256 = %s, want %s; supplied order must be kept", i, got, expectedMap.DemoSHA256)
		}
		if got := series.Maps[i].Analysis.Map.MapName; got != expectedMap.ParserMapName {
			t.Errorf("maps[%d] analysis map = %q, want %q", i, got, expectedMap.ParserMapName)
		}
	}
}

func assertHLTVSeriesTeams(t *testing.T, series *ProcessedSeries, oracle hltvOracle) {
	t.Helper()
	if len(series.Teams) != 2 {
		t.Fatalf("series has %d teams, want 2", len(series.Teams))
	}
	expectations := hltvSeriesExpectations(t, oracle)
	totalRounds := 0
	for _, expected := range expectations {
		team, found := findSeriesTeamByRoster(series.Teams, expected.steamIDs)
		if !found {
			t.Errorf("no series team has %s's roster %v; teams %+v", expected.name, expected.steamIDs, series.Teams)
			continue
		}
		if team.Name != expected.name {
			t.Errorf("series team with %s's roster is named %q", expected.name, team.Name)
		}
		if team.MapsWon != expected.mapsWon || team.RoundsWon != expected.roundsWon {
			t.Errorf("series team %s = %d maps/%d rounds, oracle says %d/%d",
				expected.name, team.MapsWon, team.RoundsWon, expected.mapsWon, expected.roundsWon)
		}
		wantThreshold := expected.mapsWon >= 2
		if isWinner := team.TeamID == series.WinnerTeamID; isWinner != wantThreshold {
			t.Errorf("series team %s winner = %t, oracle map wins say %t", expected.name, isWinner, wantThreshold)
		}
		totalRounds += expected.roundsWon
	}
	gotRounds := 0
	for _, seriesMap := range series.Maps {
		gotRounds += seriesMap.Analysis.Map.TotalRounds
	}
	if gotRounds != totalRounds {
		t.Errorf("series rounds = %d, oracle says %d", gotRounds, totalRounds)
	}
}

func findSeriesTeamByRoster(teams []SeriesTeam, steamIDs []uint64) (SeriesTeam, bool) {
	for _, team := range teams {
		if slices.Equal(team.Roster, steamIDs) {
			return team, true
		}
	}
	return SeriesTeam{}, false
}

// assertHLTVSeriesPlayers checks cross-map identity and the aggregation
// arithmetic: all ten oracle SteamIDs appear once, played every map with the
// full series-round denominator, their additive totals equal the sums of
// the standalone analyses, their rates divide exact numerators by that
// denominator, and their rating was recomputed rather than omitted.
func assertHLTVSeriesPlayers(t *testing.T, series *ProcessedSeries, inputs []SeriesMapInput, oracle hltvOracle) {
	t.Helper()
	totalRounds := 0
	for _, input := range inputs {
		totalRounds += input.Demo.Map.TotalRounds
	}
	if len(series.Players) != 10 {
		t.Errorf("series has %d aggregate players, want the ten oracle SteamIDs", len(series.Players))
	}
	for _, expectedPlayer := range oracle.Maps[0].Players {
		player := series.Players[expectedPlayer.SteamID]
		if player == nil {
			t.Errorf("aggregate player %d (%s) missing", expectedPlayer.SteamID, expectedPlayer.HLTVName)
			continue
		}
		if player.MapsPlayed != len(inputs) || player.Rounds != totalRounds {
			t.Errorf("%s maps/rounds = %d/%d, want %d/%d",
				expectedPlayer.HLTVName, player.MapsPlayed, player.Rounds, len(inputs), totalRounds)
		}

		var kills, deaths, damage, headshots, assists, kastRounds int
		for _, input := range inputs {
			mapPlayer := input.Demo.Players[expectedPlayer.SteamID]
			if mapPlayer == nil {
				t.Errorf("%s is missing from a map analysis; every oracle player plays all three maps", expectedPlayer.HLTVName)
				continue
			}
			kills += mapPlayer.KillStats.Total
			deaths += mapPlayer.Deaths
			damage += mapPlayer.AssistStats.DamageGiven
			headshots += mapPlayer.KillStats.HeadShots
			assists += mapPlayer.AssistStats.Total
			kastRounds += input.Demo.aggFacts[expectedPlayer.SteamID].kastRounds.Total
		}
		if player.KillStats.Total != kills || player.Deaths != deaths ||
			player.AssistStats.DamageGiven != damage ||
			player.KillStats.HeadShots != headshots || player.AssistStats.Total != assists {
			t.Errorf("%s additive totals %d/%d/%d/%d/%d do not equal the map sums %d/%d/%d/%d/%d",
				expectedPlayer.HLTVName,
				player.KillStats.Total, player.Deaths, player.AssistStats.DamageGiven,
				player.KillStats.HeadShots, player.AssistStats.Total,
				kills, deaths, damage, headshots, assists)
		}
		if got, want := player.AssistStats.ADR, perRound(damage, totalRounds); got != want {
			t.Errorf("%s series ADR = %v, want damage over the %d series rounds (%v)",
				expectedPlayer.HLTVName, got, totalRounds, want)
		}
		if got, want := player.PlayerStats.KAST, 100*perRound(kastRounds, totalRounds); got != want {
			t.Errorf("%s series KAST = %v, want the exact %d/%d recomputation (%v)",
				expectedPlayer.HLTVName, got, kastRounds, totalRounds, want)
		}
		if player.Rating == nil {
			t.Errorf("%s series rating omitted; parsed maps carry raw facts, so it must be recomputed", expectedPlayer.HLTVName)
		}
	}
}

// hltvParsedDemos caches checksum-verified parses per demo path for the
// test binary's lifetime, so the map and series regressions share one parse
// of each multi-hundred-MB demo instead of repeating seconds of work. Both
// only read the result: the assertions never write, and BuildSeries
// documents that it does not mutate its inputs.
var hltvParsedDemos = struct {
	sync.Mutex
	byPath map[string]*ProcessedDemo
}{byPath: map[string]*ProcessedDemo{}}

func parseVerifiedHLTVDemo(t *testing.T, path, wantSHA256 string) *ProcessedDemo {
	t.Helper()
	hltvParsedDemos.Lock()
	defer hltvParsedDemos.Unlock()
	if demo, ok := hltvParsedDemos.byPath[path]; ok {
		return demo
	}
	if err := verifyDemoChecksum(path, wantSHA256); err != nil {
		t.Fatalf("verifying HLTV demo: %v", err)
	}
	demo, err := ProcessDemo(path)
	if err != nil {
		t.Fatalf("ProcessDemo(%s): %v", path, err)
	}
	hltvParsedDemos.byPath[path] = demo
	return demo
}

func configuredDemoDirs(value string) []string {
	var dirs []string
	for _, dir := range filepath.SplitList(value) {
		if dir = strings.TrimSpace(dir); dir != "" {
			dirs = append(dirs, dir)
		}
	}
	return dirs
}

func demoDirsEnv(extra bool) string {
	if extra {
		return hltvExtraDemoDirsEnv
	}
	return hltvDemoDirEnv
}

func findDemo(dirs []string, name string) (string, error) {
	for _, dir := range dirs {
		path := filepath.Join(dir, name)
		_, err := os.Stat(path)
		if err == nil {
			return path, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("checking %s: %w", path, err)
		}
	}
	return "", fmt.Errorf("%s not found in %v: %w", name, dirs, os.ErrNotExist)
}

func observeHLTVParity(parity *hltvParity, player *DemoPlayer, expectedMap hltvMapExpected, expected hltvPlayerExpected) {
	if player.KillStats.Total == expected.Kills {
		parity.Kills++
	}
	if player.Deaths == expected.Deaths {
		parity.Deaths++
	}
	if fmt.Sprintf("%.1f", player.AssistStats.ADR) == fmt.Sprintf("%.1f", expected.ADR) {
		parity.ADR++
	}
	if kastRounds(player.PlayerMapStats.KAST, expectedMap.Rounds) == kastRounds(expected.KASTPercent, expectedMap.Rounds) {
		parity.KAST++
	}
}

func logHLTVParity(t *testing.T, label string, parity hltvParity, ratings []ratingObservation) {
	t.Helper()
	if len(ratings) == 0 {
		return
	}
	rows := len(ratings)
	t.Logf("HLTV parity, %s (%d player-maps): kills=%d/%d deaths=%d/%d ADR=%d/%d KAST=%d/%d",
		label, rows, parity.Kills, rows, parity.Deaths, rows, parity.ADR, rows, parity.KAST, rows)
	quality := summarizeRatingQuality(ratings)
	t.Logf("Rating 3.0 quality, %s (%d player-maps): MAE=%.3f RMSE=%.3f bias=%+.3f Spearman=%.3f; within ±0.05=%d, ±0.10=%d, ±0.20=%d",
		label, rows, quality.MAE, quality.RMSE, quality.Bias, quality.Spearman,
		quality.Within005, quality.Within010, quality.Within020)
}

func TestSummarizeRatingQuality(t *testing.T) {
	quality := summarizeRatingQuality([]ratingObservation{
		{Tool: 1.00, HLTV: 1.00},
		{Tool: 1.10, HLTV: 1.20},
		{Tool: 1.40, HLTV: 1.20},
	})

	checks := []struct {
		name string
		got  float64
		want float64
	}{
		{name: "MAE", got: quality.MAE, want: 0.1},
		{name: "RMSE", got: quality.RMSE, want: math.Sqrt(0.05 / 3)},
		{name: "bias", got: quality.Bias, want: 1.0 / 30},
		{name: "Spearman", got: quality.Spearman, want: math.Sqrt(3) / 2},
	}
	for _, check := range checks {
		if math.Abs(check.got-check.want) > 1e-12 {
			t.Errorf("%s = %v, want %v", check.name, check.got, check.want)
		}
	}
	if quality.Within005 != 1 || quality.Within010 != 2 || quality.Within020 != 3 {
		t.Errorf("threshold counts = (%d, %d, %d), want (1, 2, 3)",
			quality.Within005, quality.Within010, quality.Within020)
	}
}

func TestKASTRounds(t *testing.T) {
	tests := []struct {
		percent float64
		rounds  int
		want    int
	}{
		{percent: 77.3, rounds: 22, want: 17},
		{percent: 68.4, rounds: 19, want: 13},
		{percent: 73.9, rounds: 23, want: 17},
	}
	for _, test := range tests {
		if got := kastRounds(test.percent, test.rounds); got != test.want {
			t.Errorf("kastRounds(%v, %d) = %d, want %d", test.percent, test.rounds, got, test.want)
		}
	}
}

func TestHLTVDifferenceKeyIncludesFixtureIdentity(t *testing.T) {
	base := hltvExpectedDifference{
		FixtureID: "fixture-a", MapID: 1, SteamID: 2, Metric: adrMetric,
	}
	keys := map[hltvDifferenceKey]bool{
		base.key(): true,
		(hltvExpectedDifference{FixtureID: "fixture-b", MapID: 1, SteamID: 2, Metric: adrMetric}).key(): true,
		(hltvExpectedDifference{FixtureID: "fixture-a", MapID: 3, SteamID: 2, Metric: adrMetric}).key(): true,
	}
	if len(keys) != 3 {
		t.Fatalf("fixture/map identities collapsed into %d expected-difference keys, want 3", len(keys))
	}
}

func TestSpearmanRankCorrelation(t *testing.T) {
	tests := []struct {
		name string
		x    []float64
		y    []float64
		want float64
	}{
		{name: "same order", x: []float64{1, 2, 3}, y: []float64{10, 20, 30}, want: 1},
		{name: "reverse order", x: []float64{1, 2, 3}, y: []float64{30, 20, 10}, want: -1},
		{name: "ties use average ranks", x: []float64{1, 1, 2}, y: []float64{3, 3, 1}, want: -1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := spearmanRankCorrelation(test.x, test.y); math.Abs(got-test.want) > 1e-12 {
				t.Errorf("spearmanRankCorrelation() = %v, want %v", got, test.want)
			}
		})
	}
}

func loadHLTVOracle(t *testing.T, path string) hltvOracle {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading HLTV oracle fixture %s: %v", path, err)
	}
	var oracle hltvOracle
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&oracle); err != nil {
		t.Fatalf("decoding HLTV oracle fixture %s: %v", path, err)
	}
	validateHLTVOracle(t, path, oracle)
	return oracle
}

func validateHLTVOracle(t *testing.T, path string, oracle hltvOracle) {
	t.Helper()
	if oracle.FixtureID == "" {
		t.Fatalf("HLTV oracle %s has an empty fixture_id", path)
	}
	if oracle.MatchID == 0 || oracle.MatchStatsURL == "" {
		t.Fatalf("HLTV oracle %s is missing match metadata", path)
	}
	if len(oracle.Maps) == 0 {
		t.Fatalf("HLTV oracle %s has no maps", path)
	}

	mapsByID := make(map[int]hltvMapExpected, len(oracle.Maps))
	for _, expectedMap := range oracle.Maps {
		if expectedMap.MapID == 0 || expectedMap.MapStatsURL == "" || expectedMap.DemoSHA256 == "" {
			t.Fatalf("HLTV oracle %s map %q is missing source metadata", path, expectedMap.Name)
		}
		// game_mode must be pinned explicitly because "" is itself a valid
		// expectation: the parser reports it for unknown modes. An omitted
		// key is an unaudited fixture, not an unknown-mode assertion.
		if expectedMap.GameMode == nil {
			t.Fatalf("HLTV oracle %s map %q has no game_mode; pin the audited mode, %q for unknown",
				path, expectedMap.Name, "")
		}
		if _, exists := mapsByID[expectedMap.MapID]; exists {
			t.Fatalf("HLTV oracle %s repeats map ID %d", path, expectedMap.MapID)
		}
		mapsByID[expectedMap.MapID] = expectedMap
		if len(expectedMap.Players) != 10 {
			t.Fatalf("HLTV oracle %s map %s has %d players, want 10", path, expectedMap.Name, len(expectedMap.Players))
		}
		seenPlayers := make(map[uint64]bool, len(expectedMap.Players))
		for _, player := range expectedMap.Players {
			if player.SteamID == 0 {
				t.Fatalf("HLTV oracle %s map %s contains a zero SteamID", path, expectedMap.Name)
			}
			if seenPlayers[player.SteamID] {
				t.Fatalf("HLTV oracle %s map %s repeats SteamID %d", path, expectedMap.Name, player.SteamID)
			}
			seenPlayers[player.SteamID] = true
		}
		// Teams must be pinned explicitly, like game_mode: they carry the
		// issue #28 logical-team contract, so an omitted key is an
		// unaudited fixture rather than a no-team assertion.
		if expectedMap.Teams == nil {
			t.Fatalf("HLTV oracle %s map %q has no teams; pin the audited rosters and per-team scores", path, expectedMap.Name)
		}
		validateHLTVTeamsFixture(t, path, expectedMap, seenPlayers)
	}
}

// validateHLTVTeamsFixture checks a map's pinned teams for internal
// consistency: two distinct named lineups of five that partition the map's
// ten players, with scores that add up to the rounds and agree with the
// existing side-based score_values.
func validateHLTVTeamsFixture(t *testing.T, path string, expectedMap hltvMapExpected, mapPlayers map[uint64]bool) {
	t.Helper()
	teams := *expectedMap.Teams
	if teams[0].Name == "" || teams[1].Name == "" || teams[0].Name == teams[1].Name {
		t.Fatalf("HLTV oracle %s map %s teams need two distinct nonempty names, got %q and %q",
			path, expectedMap.Name, teams[0].Name, teams[1].Name)
	}
	onTeams := make(map[uint64]bool, len(mapPlayers))
	for _, team := range teams {
		if len(team.SteamIDs) != 5 {
			t.Fatalf("HLTV oracle %s map %s team %s has %d SteamIDs, want 5",
				path, expectedMap.Name, team.Name, len(team.SteamIDs))
		}
		for _, id := range team.SteamIDs {
			if !mapPlayers[id] {
				t.Fatalf("HLTV oracle %s map %s team %s lists SteamID %d that is not among the map's players",
					path, expectedMap.Name, team.Name, id)
			}
			if onTeams[id] {
				t.Fatalf("HLTV oracle %s map %s lists SteamID %d on both teams", path, expectedMap.Name, id)
			}
			onTeams[id] = true
		}
	}
	if got, want := sortedScore(teams[0].Score, teams[1].Score), sortedScore(expectedMap.ScoreValues[0], expectedMap.ScoreValues[1]); got != want {
		t.Fatalf("HLTV oracle %s map %s team scores %v disagree with score_values %v", path, expectedMap.Name, got, want)
	}
	if teams[0].Score+teams[1].Score != expectedMap.Rounds {
		t.Fatalf("HLTV oracle %s map %s team scores add to %d, want the %d rounds",
			path, expectedMap.Name, teams[0].Score+teams[1].Score, expectedMap.Rounds)
	}
}

func validateHLTVOracleSet(t *testing.T, specs []hltvOracleSpec, oracles []hltvOracle) {
	t.Helper()
	if len(specs) != len(oracles) {
		t.Fatalf("HLTV oracle specs = %d, loaded oracles = %d", len(specs), len(oracles))
	}
	seenFixtures := make(map[string]bool, len(oracles))
	totalMaps, totalRows := 0, 0
	for i, oracle := range oracles {
		if seenFixtures[oracle.FixtureID] {
			t.Fatalf("HLTV oracles repeat fixture_id %q", oracle.FixtureID)
		}
		seenFixtures[oracle.FixtureID] = true
		if got, want := len(oracle.Maps), specs[i].ExpectedMaps; got != want {
			t.Fatalf("HLTV oracle %s has %d maps, want %d", specs[i].Path, got, want)
		}
		totalMaps += len(oracle.Maps)
		for _, expectedMap := range oracle.Maps {
			totalRows += len(expectedMap.Players)
		}
	}
	if totalMaps != 8 || totalRows != 80 {
		t.Fatalf("HLTV oracle coverage = %d maps/%d player-maps, want 8/80", totalMaps, totalRows)
	}
}

func validateHLTVExpectedDifferences(t *testing.T, oracles []hltvOracle) {
	t.Helper()
	mapsByKey := make(map[hltvFixtureMapKey]hltvMapExpected)
	for _, oracle := range oracles {
		for _, expectedMap := range oracle.Maps {
			key := hltvFixtureMapKey{FixtureID: oracle.FixtureID, MapID: expectedMap.MapID}
			if _, exists := mapsByKey[key]; exists {
				t.Fatalf("HLTV oracles repeat fixture/map key %s/%d", oracle.FixtureID, expectedMap.MapID)
			}
			mapsByKey[key] = expectedMap
		}
	}
	seenDifferences := make(map[hltvDifferenceKey]bool, len(hltvExpectedDifferences))
	for _, difference := range hltvExpectedDifferences {
		if seenDifferences[difference.key()] {
			t.Fatalf("expected-difference table repeats %+v", difference.key())
		}
		seenDifferences[difference.key()] = true
		expectedMap, exists := mapsByKey[hltvFixtureMapKey{FixtureID: difference.FixtureID, MapID: difference.MapID}]
		if !exists {
			t.Fatalf("expected difference references unknown fixture/map %q/%d", difference.FixtureID, difference.MapID)
		}
		expectedPlayer, exists := findExpectedPlayer(expectedMap, difference.SteamID)
		if !exists {
			t.Fatalf("expected difference references unknown SteamID %d on %s/%d",
				difference.SteamID, difference.FixtureID, difference.MapID)
		}
		wantValue := expectedMetricValue(t, expectedMap, expectedPlayer, difference.Metric)
		if difference.HLTVValue != wantValue {
			t.Fatalf("expected difference %s/%d/%d/%s has HLTV value %q, fixture says %q",
				difference.FixtureID, difference.MapID, difference.SteamID, difference.Metric, difference.HLTVValue, wantValue)
		}
		if difference.ToolValue == difference.HLTVValue {
			t.Fatalf("expected difference %s/%d/%d/%s has identical HLTV and tool values %q",
				difference.FixtureID, difference.MapID, difference.SteamID, difference.Metric, difference.HLTVValue)
		}
		if difference.Metric != adrMetric && difference.Metric != kastMetric {
			t.Fatalf("expected difference %s/%d/%d uses unsupported metric %q",
				difference.FixtureID, difference.MapID, difference.SteamID, difference.Metric)
		}
		if difference.FollowUp == "" {
			t.Fatalf("expected difference %s/%d/%d/%s has no follow-up reference",
				difference.FixtureID, difference.MapID, difference.SteamID, difference.Metric)
		}
	}
}

func verifyDemoChecksum(path, want string) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("opening %s: %w", path, err)
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return fmt.Errorf("hashing %s: %w", path, err)
	}
	got := hex.EncodeToString(hash.Sum(nil))
	if got != want {
		return fmt.Errorf("%s has SHA-256 %s, want %s; use the exact audited demo named by the oracle", path, got, want)
	}
	return nil
}

func assertHLTVMap(t *testing.T, fixtureID string, result *ProcessedDemo, expected hltvMapExpected) {
	t.Helper()
	label := fmt.Sprintf("%s/%d/%s", fixtureID, expected.MapID, expected.Name)
	if result.Map.MapName != expected.ParserMapName {
		t.Errorf("%s map name = %q, want %q", label, result.Map.MapName, expected.ParserMapName)
	}
	if result.GameMode != *expected.GameMode {
		t.Errorf("%s game mode = %q, want %q", label, result.GameMode, *expected.GameMode)
	}
	if result.Map.TotalRounds != expected.Rounds {
		t.Errorf("%s rounds = %d, want %d", label, result.Map.TotalRounds, expected.Rounds)
	}
	gotScore := sortedScore(result.Map.RoundsWonCT, result.Map.RoundsWonT)
	wantScore := sortedScore(expected.ScoreValues[0], expected.ScoreValues[1])
	if gotScore != wantScore {
		t.Errorf("%s score values = %v, want %v", label, gotScore, wantScore)
	}
}

func assertHLTVRoster(t *testing.T, fixtureID string, players map[uint64]*DemoPlayer, expected hltvMapExpected) {
	t.Helper()
	expectedPlayers := make(map[uint64]string, len(expected.Players))
	for _, player := range expected.Players {
		expectedPlayers[player.SteamID] = player.HLTVName
	}

	var missing, unexpected []string
	for steamID, hltvName := range expectedPlayers {
		if players[steamID] == nil {
			missing = append(missing, fmt.Sprintf("%d (%s)", steamID, hltvName))
		}
	}
	for steamID, player := range players {
		if _, exists := expectedPlayers[steamID]; !exists {
			unexpected = append(unexpected, fmt.Sprintf("%d (%s)", steamID, player.Name))
		}
	}
	sort.Strings(missing)
	sort.Strings(unexpected)
	if len(players) != 10 || len(missing) > 0 || len(unexpected) > 0 {
		t.Fatalf("%s/%d/%s roster mismatch: got %d players; missing=%v unexpected=%v",
			fixtureID, expected.MapID, expected.Name,
			len(players), missing, unexpected)
	}
}

// assertHLTVTeams checks the issue #28 logical-team contract on a real
// tournament demo: exactly two teams whose rosters, display names, aliases
// and per-team scores survive every side switch, whose scores reconcile with
// the final side-based scoreboard, and whose players reference them through
// team_id. Parsed teams are matched to the fixture rows by exact roster, so
// the assertion is independent of the parser's map-local team numbering.
func assertHLTVTeams(t *testing.T, fixtureID string, result *ProcessedDemo, expected hltvMapExpected) {
	t.Helper()
	label := fmt.Sprintf("%s/%d/%s", fixtureID, expected.MapID, expected.Name)
	if len(result.Teams) != 2 {
		t.Errorf("%s parsed %d teams, want 2", label, len(result.Teams))
		return
	}
	gotScores := sortedScore(result.Teams[0].Score, result.Teams[1].Score)
	sideScores := sortedScore(result.Map.RoundsWonCT, result.Map.RoundsWonT)
	if gotScores != sideScores {
		t.Errorf("%s team scores %v do not reconcile with final side scores %v", label, gotScores, sideScores)
	}
	for _, expectedTeam := range *expected.Teams {
		team, found := findTeamByRoster(result.Teams, expectedTeam.SteamIDs)
		if !found {
			t.Errorf("%s no parsed team has %s's roster %v; parsed teams %+v",
				label, expectedTeam.Name, sortedIDs(expectedTeam.SteamIDs), result.Teams)
			continue
		}
		if team.Name != expectedTeam.Name {
			t.Errorf("%s team with %s's roster is named %q, want %q",
				label, expectedTeam.Name, team.Name, expectedTeam.Name)
		}
		if !slices.Equal(team.Aliases, []string{expectedTeam.Name}) {
			t.Errorf("%s team %s aliases = %v, want exactly the observed clan name",
				label, expectedTeam.Name, team.Aliases)
		}
		if team.Score != expectedTeam.Score {
			t.Errorf("%s team %s score = %d, want %d", label, expectedTeam.Name, team.Score, expectedTeam.Score)
		}
		for _, id := range expectedTeam.SteamIDs {
			// A missing player is already reported by assertHLTVRoster.
			if player := result.Players[id]; player != nil && player.TeamID != team.TeamID {
				t.Errorf("%s player %d team_id = %d, want %d (%s)",
					label, id, player.TeamID, team.TeamID, expectedTeam.Name)
			}
		}
	}
}

func findTeamByRoster(teams []DemoTeam, steamIDs []uint64) (DemoTeam, bool) {
	want := sortedIDs(steamIDs)
	for _, team := range teams {
		if slices.Equal(team.Roster, want) {
			return team, true
		}
	}
	return DemoTeam{}, false
}

func assertHLTVPlayer(t *testing.T, fixtureID string, expectedMap hltvMapExpected, expected hltvPlayerExpected, player *DemoPlayer) {
	t.Helper()
	if player.SteamID != expected.SteamID {
		t.Errorf("%s/%d/%s SteamID = %d, want %d",
			fixtureID, expectedMap.MapID, expected.HLTVName, player.SteamID, expected.SteamID)
	}
	if player.KillStats.Total != expected.Kills {
		t.Errorf("%s/%d/%s (%d) kills = %d, HLTV %d", fixtureID, expectedMap.MapID, expected.HLTVName,
			expected.SteamID, player.KillStats.Total, expected.Kills)
	}
	if player.Deaths != expected.Deaths {
		t.Errorf("%s/%d/%s (%d) deaths = %d, HLTV %d", fixtureID, expectedMap.MapID, expected.HLTVName,
			expected.SteamID, player.Deaths, expected.Deaths)
	}

	assertHLTVMetric(t, hltvDifferenceKey{fixtureID, expectedMap.MapID, expected.SteamID, adrMetric},
		fmt.Sprintf("%.1f", expected.ADR), fmt.Sprintf("%.1f", player.AssistStats.ADR), expected.HLTVName)
	wantKASTRounds := kastRounds(expected.KASTPercent, expectedMap.Rounds)
	gotKASTRounds := kastRounds(player.PlayerMapStats.KAST, expectedMap.Rounds)
	assertHLTVMetric(t, hltvDifferenceKey{fixtureID, expectedMap.MapID, expected.SteamID, kastMetric},
		formatKAST(wantKASTRounds, expectedMap.Rounds, expected.KASTPercent),
		formatKAST(gotKASTRounds, expectedMap.Rounds, player.PlayerMapStats.KAST), expected.HLTVName)
}

func assertHLTVMetric(t *testing.T, key hltvDifferenceKey, want, got, hltvName string) {
	t.Helper()
	difference, known := findExpectedDifference(key)
	if !known {
		if got != want {
			t.Errorf("%s/%d/%s (%d) %s = %s, HLTV %s",
				key.FixtureID, key.MapID, hltvName, key.SteamID, key.Metric, got, want)
		}
		return
	}
	if difference.HLTVValue != want {
		t.Fatalf("%s/%d/%s (%d) %s exception says HLTV %s, fixture says %s",
			key.FixtureID, key.MapID, hltvName, key.SteamID, key.Metric, difference.HLTVValue, want)
	}
	if got == want {
		t.Errorf("%s/%d/%s (%d) %s now matches HLTV at %s; remove the stale exception (%s) and promote this row to strict parity",
			key.FixtureID, key.MapID, hltvName, key.SteamID, key.Metric, got, difference.FollowUp)
		return
	}
	if got != difference.ToolValue {
		t.Errorf("%s/%d/%s (%d) %s = %s, want current tool value %s (HLTV %s; %s)",
			key.FixtureID, key.MapID, hltvName, key.SteamID, key.Metric, got, difference.ToolValue,
			difference.HLTVValue, difference.FollowUp)
	}
}

func expectedMetricValue(t *testing.T, expectedMap hltvMapExpected, player hltvPlayerExpected, metric string) string {
	t.Helper()
	switch metric {
	case adrMetric:
		return fmt.Sprintf("%.1f", player.ADR)
	case kastMetric:
		return formatKAST(kastRounds(player.KASTPercent, expectedMap.Rounds), expectedMap.Rounds, player.KASTPercent)
	default:
		t.Fatalf("expected difference uses unsupported metric %q", metric)
		return ""
	}
}

func findExpectedDifference(key hltvDifferenceKey) (hltvExpectedDifference, bool) {
	for _, difference := range hltvExpectedDifferences {
		if difference.key() == key {
			return difference, true
		}
	}
	return hltvExpectedDifference{}, false
}

func findExpectedPlayer(expectedMap hltvMapExpected, steamID uint64) (hltvPlayerExpected, bool) {
	for _, player := range expectedMap.Players {
		if player.SteamID == steamID {
			return player, true
		}
	}
	return hltvPlayerExpected{}, false
}

func sortedScore(a, b int) [2]int {
	if a > b {
		return [2]int{b, a}
	}
	return [2]int{a, b}
}

func kastRounds(percent float64, rounds int) int {
	return int(math.Round(percent * float64(rounds) / 100))
}

func formatKAST(qualifyingRounds, rounds int, percent float64) string {
	return fmt.Sprintf("%d/%d (%.1f%%)", qualifyingRounds, rounds, percent)
}

func displayedRating(value float64) float64 {
	formatted := fmt.Sprintf("%.2f", value)
	result, err := strconv.ParseFloat(formatted, 64)
	if err != nil {
		panic(fmt.Sprintf("parsing displayed float %q: %v", formatted, err))
	}
	return result
}

func summarizeRatingQuality(observations []ratingObservation) ratingQuality {
	var quality ratingQuality
	toolValues := make([]float64, len(observations))
	hltvValues := make([]float64, len(observations))
	for i, observation := range observations {
		delta := observation.Tool - observation.HLTV
		absoluteDelta := math.Abs(delta)
		quality.MAE += absoluteDelta
		quality.RMSE += delta * delta
		quality.Bias += delta
		if absoluteDelta <= 0.05+1e-12 {
			quality.Within005++
		}
		if absoluteDelta <= 0.10+1e-12 {
			quality.Within010++
		}
		if absoluteDelta <= 0.20+1e-12 {
			quality.Within020++
		}
		toolValues[i] = observation.Tool
		hltvValues[i] = observation.HLTV
	}
	if len(observations) == 0 {
		return quality
	}
	count := float64(len(observations))
	quality.MAE /= count
	quality.RMSE = math.Sqrt(quality.RMSE / count)
	quality.Bias /= count
	quality.Spearman = spearmanRankCorrelation(toolValues, hltvValues)
	return quality
}

func spearmanRankCorrelation(x, y []float64) float64 {
	if len(x) == 0 || len(x) != len(y) {
		return 0
	}
	xRanks := averageRanks(x)
	yRanks := averageRanks(y)
	meanRank := float64(len(x)+1) / 2
	var covariance, xVariance, yVariance float64
	for i := range xRanks {
		xDelta := xRanks[i] - meanRank
		yDelta := yRanks[i] - meanRank
		covariance += xDelta * yDelta
		xVariance += xDelta * xDelta
		yVariance += yDelta * yDelta
	}
	if xVariance == 0 || yVariance == 0 {
		return 0
	}
	return covariance / math.Sqrt(xVariance*yVariance)
}

func averageRanks(values []float64) []float64 {
	type rankedValue struct {
		value float64
		index int
	}
	ranked := make([]rankedValue, len(values))
	for i, value := range values {
		ranked[i] = rankedValue{value: value, index: i}
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		return ranked[i].value < ranked[j].value
	})

	ranks := make([]float64, len(values))
	for start := 0; start < len(ranked); {
		end := start + 1
		for end < len(ranked) && ranked[end].value == ranked[start].value {
			end++
		}
		average := float64(start+1+end) / 2
		for i := start; i < end; i++ {
			ranks[ranked[i].index] = average
		}
		start = end
	}
	return ranks
}
