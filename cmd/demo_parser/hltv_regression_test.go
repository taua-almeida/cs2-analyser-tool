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
	"sort"
	"strconv"
	"strings"
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
	Rounds        int                  `json:"rounds"`
	ScoreValues   [2]int               `json:"score_values"`
	Players       []hltvPlayerExpected `json:"players"`
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
				if err := verifyDemoChecksum(demoPath, expectedMap.DemoSHA256); err != nil {
					t.Fatalf("verifying HLTV demo: %v", err)
				}

				result, err := ProcessDemo(demoPath)
				if err != nil {
					t.Fatalf("ProcessDemo(%s): %v", expectedMap.DemoFile, err)
				}

				assertHLTVMap(t, oracle.FixtureID, result, expectedMap)
				assertHLTVRoster(t, oracle.FixtureID, result.Players, expectedMap)
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
