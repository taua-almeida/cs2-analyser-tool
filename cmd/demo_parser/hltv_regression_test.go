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
	hltvDemoDirEnv        = "HLTV_DEMO_DIR"
	requireHLTVDemosEnv   = "REQUIRE_HLTV_DEMOS"
	hltvOracleFixturePath = "testdata/hltv-129241/expected.json"
	adrMetric             = "ADR"
	kastMetric            = "KAST qualifying rounds"
)

type hltvOracle struct {
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
	MapName string
	SteamID uint64
	Metric  string
}

type hltvExpectedDifference struct {
	MapName   string
	SteamID   uint64
	Metric    string
	HLTVValue string
	ToolValue string
	Issue     int
}

func (d hltvExpectedDifference) key() hltvDifferenceKey {
	return hltvDifferenceKey{MapName: d.MapName, SteamID: d.SteamID, Metric: d.Metric}
}

// These are known parser differences, not tolerances. Each row pins both
// sides of the current discrepancy: a new value fails, and reaching parity
// also fails until the stale exception is removed.
var hltvExpectedDifferences = []hltvExpectedDifference{
	{MapName: "inferno", SteamID: 76561198158971650, Metric: kastMetric, HLTVValue: "17/22 (77.3%)", ToolValue: "18/22 (81.8%)", Issue: 38},
	{MapName: "inferno", SteamID: 76561198050779850, Metric: kastMetric, HLTVValue: "14/22 (63.6%)", ToolValue: "16/22 (72.7%)", Issue: 38},
	{MapName: "inferno", SteamID: 76561198127259887, Metric: kastMetric, HLTVValue: "14/22 (63.6%)", ToolValue: "15/22 (68.2%)", Issue: 38},
	{MapName: "inferno", SteamID: 76561198081702155, Metric: kastMetric, HLTVValue: "15/22 (68.2%)", ToolValue: "16/22 (72.7%)", Issue: 38},
	{MapName: "inferno", SteamID: 76561198044112269, Metric: kastMetric, HLTVValue: "16/22 (72.7%)", ToolValue: "18/22 (81.8%)", Issue: 38},
	{MapName: "inferno", SteamID: 76561199048086137, Metric: kastMetric, HLTVValue: "18/22 (81.8%)", ToolValue: "17/22 (77.3%)", Issue: 38},
	{MapName: "inferno", SteamID: 76561198226777692, Metric: kastMetric, HLTVValue: "15/22 (68.2%)", ToolValue: "16/22 (72.7%)", Issue: 38},
	{MapName: "inferno", SteamID: 76561198208618504, Metric: kastMetric, HLTVValue: "16/22 (72.7%)", ToolValue: "17/22 (77.3%)", Issue: 38},
	{MapName: "anubis", SteamID: 76561198081702155, Metric: kastMetric, HLTVValue: "13/19 (68.4%)", ToolValue: "12/19 (63.2%)", Issue: 38},
	{MapName: "anubis", SteamID: 76561198108660703, Metric: kastMetric, HLTVValue: "14/19 (73.7%)", ToolValue: "15/19 (78.9%)", Issue: 38},
	{MapName: "anubis", SteamID: 76561198044112269, Metric: kastMetric, HLTVValue: "15/19 (78.9%)", ToolValue: "16/19 (84.2%)", Issue: 38},
	{MapName: "mirage", SteamID: 76561198108660703, Metric: kastMetric, HLTVValue: "17/23 (73.9%)", ToolValue: "18/23 (78.3%)", Issue: 38},
	{MapName: "mirage", SteamID: 76561199048086137, Metric: kastMetric, HLTVValue: "17/23 (73.9%)", ToolValue: "19/23 (82.6%)", Issue: 38},
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
	oracle := loadHLTVOracle(t)
	demoDir := os.Getenv(hltvDemoDirEnv)
	requireDemos := os.Getenv(requireHLTVDemosEnv) != ""
	if demoDir == "" {
		if requireDemos {
			t.Fatalf("%s is set but %s is empty; point it at the directory containing the three match 129241 demos",
				requireHLTVDemosEnv, hltvDemoDirEnv)
		}
		t.Skipf("HLTV regression demos are not configured; set %s to run the external-oracle harness", hltvDemoDirEnv)
	}

	var ratings []ratingObservation
	var parity hltvParity
	for _, expectedMap := range oracle.Maps {
		t.Run(expectedMap.Name, func(t *testing.T) {
			demoPath := filepath.Join(demoDir, expectedMap.DemoFile)
			if err := verifyDemoChecksum(demoPath, expectedMap.DemoSHA256); err != nil {
				if errors.Is(err, os.ErrNotExist) && !requireDemos {
					t.Skipf("HLTV demo %s is absent; set %s to make missing demos fail", demoPath, requireHLTVDemosEnv)
				}
				t.Fatalf("verifying HLTV demo: %v", err)
			}

			result, err := ProcessDemo(demoPath)
			if err != nil {
				t.Fatalf("ProcessDemo(%s): %v", expectedMap.DemoFile, err)
			}

			assertHLTVMap(t, result, expectedMap)
			assertHLTVRoster(t, result.Players, expectedMap)
			for _, expectedPlayer := range expectedMap.Players {
				player := result.Players[expectedPlayer.SteamID]
				assertHLTVPlayer(t, expectedMap, expectedPlayer, player)
				if player.KillStats.Total == expectedPlayer.Kills {
					parity.Kills++
				}
				if player.Deaths == expectedPlayer.Deaths {
					parity.Deaths++
				}
				if fmt.Sprintf("%.1f", player.AssistStats.ADR) == fmt.Sprintf("%.1f", expectedPlayer.ADR) {
					parity.ADR++
				}
				if kastRounds(player.PlayerMapStats.KAST, expectedMap.Rounds) ==
					kastRounds(expectedPlayer.KASTPercent, expectedMap.Rounds) {
					parity.KAST++
				}
				ratings = append(ratings, ratingObservation{
					Tool: displayedRating(player.Rating.Value),
					HLTV: expectedPlayer.Rating,
				})
			}
		})
	}

	if len(ratings) > 0 {
		rows := len(ratings)
		t.Logf("HLTV parity (%d player-maps): kills=%d/%d deaths=%d/%d ADR=%d/%d KAST=%d/%d",
			rows, parity.Kills, rows, parity.Deaths, rows, parity.ADR, rows, parity.KAST, rows)
		quality := summarizeRatingQuality(ratings)
		t.Logf("Rating 3.0 quality (%d player-maps): MAE=%.3f RMSE=%.3f bias=%+.3f Spearman=%.3f; within ±0.05=%d, ±0.10=%d, ±0.20=%d",
			rows, quality.MAE, quality.RMSE, quality.Bias, quality.Spearman,
			quality.Within005, quality.Within010, quality.Within020)
	}
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

func loadHLTVOracle(t *testing.T) hltvOracle {
	t.Helper()
	data, err := os.ReadFile(hltvOracleFixturePath)
	if err != nil {
		t.Fatalf("reading HLTV oracle fixture: %v", err)
	}
	var oracle hltvOracle
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&oracle); err != nil {
		t.Fatalf("decoding HLTV oracle fixture: %v", err)
	}
	validateHLTVOracle(t, oracle)
	return oracle
}

func validateHLTVOracle(t *testing.T, oracle hltvOracle) {
	t.Helper()
	if oracle.MatchID != 129241 {
		t.Fatalf("HLTV oracle match_id = %d, want 129241", oracle.MatchID)
	}
	if len(oracle.Maps) != 3 {
		t.Fatalf("HLTV oracle has %d maps, want 3", len(oracle.Maps))
	}

	mapsByName := make(map[string]hltvMapExpected, len(oracle.Maps))
	for _, expectedMap := range oracle.Maps {
		if _, exists := mapsByName[expectedMap.Name]; exists {
			t.Fatalf("HLTV oracle repeats map %q", expectedMap.Name)
		}
		mapsByName[expectedMap.Name] = expectedMap
		if len(expectedMap.Players) != 10 {
			t.Fatalf("HLTV oracle map %s has %d players, want 10", expectedMap.Name, len(expectedMap.Players))
		}
		seenPlayers := make(map[uint64]bool, len(expectedMap.Players))
		for _, player := range expectedMap.Players {
			if player.SteamID == 0 {
				t.Fatalf("HLTV oracle map %s contains a zero SteamID", expectedMap.Name)
			}
			if seenPlayers[player.SteamID] {
				t.Fatalf("HLTV oracle map %s repeats SteamID %d", expectedMap.Name, player.SteamID)
			}
			seenPlayers[player.SteamID] = true
		}
	}

	seenDifferences := make(map[hltvDifferenceKey]bool, len(hltvExpectedDifferences))
	for _, difference := range hltvExpectedDifferences {
		if seenDifferences[difference.key()] {
			t.Fatalf("expected-difference table repeats %+v", difference.key())
		}
		seenDifferences[difference.key()] = true
		expectedMap, exists := mapsByName[difference.MapName]
		if !exists {
			t.Fatalf("expected difference references unknown map %q", difference.MapName)
		}
		expectedPlayer, exists := findExpectedPlayer(expectedMap, difference.SteamID)
		if !exists {
			t.Fatalf("expected difference references unknown SteamID %d on %s", difference.SteamID, difference.MapName)
		}
		wantValue := expectedMetricValue(t, expectedMap, expectedPlayer, difference.Metric)
		if difference.HLTVValue != wantValue {
			t.Fatalf("expected difference %s/%d/%s has HLTV value %q, fixture says %q",
				difference.MapName, difference.SteamID, difference.Metric, difference.HLTVValue, wantValue)
		}
		if difference.ToolValue == difference.HLTVValue {
			t.Fatalf("expected difference %s/%d/%s has identical HLTV and tool values %q",
				difference.MapName, difference.SteamID, difference.Metric, difference.HLTVValue)
		}
		if difference.Metric != kastMetric {
			t.Fatalf("expected difference %s/%d uses unsupported metric %q; only issue #38 KAST differences are allowed",
				difference.MapName, difference.SteamID, difference.Metric)
		}
		if difference.Issue != 38 {
			t.Fatalf("expected difference %s/%d/%s points to issue #%d", difference.MapName,
				difference.SteamID, difference.Metric, difference.Issue)
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
		return fmt.Errorf("%s has SHA-256 %s, want %s; use the exact match 129241 demo", path, got, want)
	}
	return nil
}

func assertHLTVMap(t *testing.T, result *ProcessedDemo, expected hltvMapExpected) {
	t.Helper()
	if result.Map.MapName != expected.ParserMapName {
		t.Errorf("map name = %q, want %q", result.Map.MapName, expected.ParserMapName)
	}
	if result.Map.TotalRounds != expected.Rounds {
		t.Errorf("%s rounds = %d, want %d", expected.Name, result.Map.TotalRounds, expected.Rounds)
	}
	gotScore := sortedScore(result.Map.RoundsWonCT, result.Map.RoundsWonT)
	wantScore := sortedScore(expected.ScoreValues[0], expected.ScoreValues[1])
	if gotScore != wantScore {
		t.Errorf("%s score values = %v, want %v", expected.Name, gotScore, wantScore)
	}
}

func assertHLTVRoster(t *testing.T, players map[uint64]*DemoPlayer, expected hltvMapExpected) {
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
		t.Fatalf("%s roster mismatch: got %d players; missing=%v unexpected=%v", expected.Name,
			len(players), missing, unexpected)
	}
}

func assertHLTVPlayer(t *testing.T, expectedMap hltvMapExpected, expected hltvPlayerExpected, player *DemoPlayer) {
	t.Helper()
	if player.SteamID != expected.SteamID {
		t.Errorf("%s/%s SteamID = %d, want %d", expectedMap.Name, expected.HLTVName, player.SteamID, expected.SteamID)
	}
	if player.KillStats.Total != expected.Kills {
		t.Errorf("%s/%s (%d) kills = %d, HLTV %d", expectedMap.Name, expected.HLTVName,
			expected.SteamID, player.KillStats.Total, expected.Kills)
	}
	if player.Deaths != expected.Deaths {
		t.Errorf("%s/%s (%d) deaths = %d, HLTV %d", expectedMap.Name, expected.HLTVName,
			expected.SteamID, player.Deaths, expected.Deaths)
	}

	assertHLTVMetric(t, hltvDifferenceKey{expectedMap.Name, expected.SteamID, adrMetric},
		fmt.Sprintf("%.1f", expected.ADR), fmt.Sprintf("%.1f", player.AssistStats.ADR), expected.HLTVName)
	wantKASTRounds := kastRounds(expected.KASTPercent, expectedMap.Rounds)
	gotKASTRounds := kastRounds(player.PlayerMapStats.KAST, expectedMap.Rounds)
	assertHLTVMetric(t, hltvDifferenceKey{expectedMap.Name, expected.SteamID, kastMetric},
		formatKAST(wantKASTRounds, expectedMap.Rounds, expected.KASTPercent),
		formatKAST(gotKASTRounds, expectedMap.Rounds, player.PlayerMapStats.KAST), expected.HLTVName)
}

func assertHLTVMetric(t *testing.T, key hltvDifferenceKey, want, got, hltvName string) {
	t.Helper()
	difference, known := findExpectedDifference(key)
	if !known {
		if got != want {
			t.Errorf("%s/%s (%d) %s = %s, HLTV %s", key.MapName, hltvName, key.SteamID, key.Metric, got, want)
		}
		return
	}
	if difference.HLTVValue != want {
		t.Fatalf("%s/%s (%d) %s exception says HLTV %s, fixture says %s",
			key.MapName, hltvName, key.SteamID, key.Metric, difference.HLTVValue, want)
	}
	if got == want {
		t.Errorf("%s/%s (%d) %s now matches HLTV at %s; remove the stale issue #%d exception and promote this row to strict parity",
			key.MapName, hltvName, key.SteamID, key.Metric, got, difference.Issue)
		return
	}
	if got != difference.ToolValue {
		t.Errorf("%s/%s (%d) %s = %s, want current tool value %s (HLTV %s, issue #%d)",
			key.MapName, hltvName, key.SteamID, key.Metric, got, difference.ToolValue,
			difference.HLTVValue, difference.Issue)
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
