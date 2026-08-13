package dataexport

import (
	"encoding/csv"
	"encoding/json"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	demoparser "github.com/taua-almeida/cs2-analyser-tool/cmd/demo_parser"
)

// The flat CSV header contract, pinned independently of playerCSVHeader so
// accidental production drift fails these tests. New columns are appended
// after all existing ones so legacy column positions stay stable; the series
// CSV test asserts the identical concatenation.
var (
	legacyCSVHeader  = []string{"Name", "Kills", "Deaths", "K/D", "HS", "Assists", "Flash Assists", "Damage Given", "ADR", "KAST (%)", "Precision (%)", "Trade Kills", "Deaths Traded", "Opening Kills", "Opening Deaths", "Opening Success (%)", "MVPs", "ACEs", "2K", "3K", "4K", "5K", "Clutches Won", "Rounds CT", "Rounds T", "Kills CT", "Kills T", "Deaths CT", "Deaths T", "Deaths Traded CT", "Deaths Traded T", "ADR CT", "ADR T", "KAST CT (%)", "KAST T (%)", "Best Weapon"}
	utilityCSVHeader = []string{"Enemies Flashed", "Friends Flashed", "Enemy Flash Time (s)", "Average Enemy Flash Time (s)", "Utility Damage Total", "HE Utility Damage", "Fire Utility Damage", "Grenades Thrown Total", "Flashbangs Thrown", "Smokes Thrown", "HE Grenades Thrown", "Molotovs Thrown", "Incendiaries Thrown", "Decoys Thrown", "Unused Utility Value"}
	ratingCSVHeader  = []string{"Rating", "Rating Kills", "Rating Damage", "Rating Survival", "Rating KAST", "Rating Multi-kill", "Rating Round Swing"}
	approxCSVHeader  = []string{"Approx. eKAST (%)", "Approx. swing (%)"}
)

func utilityPlayer() *demoparser.DemoPlayer {
	return &demoparser.DemoPlayer{
		SteamID: 1,
		Name:    "utility-player",
		KillStats: demoparser.KillStats{
			Total:        1,
			WeaponsKills: map[string]int{"AK-47": 1},
		},
		AssistStats: demoparser.AssistStats{FlashedEnemies: 2},
		UtilityStats: demoparser.UtilityStats{
			EnemiesFlashed:               3,
			FriendsFlashed:               4,
			EnemyFlashTimeSeconds:        5.25,
			AverageEnemyFlashTimeSeconds: 1.75,
			UtilityDamage:                demoparser.UtilityDamageStats{Total: 60, HE: 40, Fire: 20},
			GrenadesThrown:               demoparser.GrenadesThrownStats{Total: 21, Flash: 1, Smoke: 2, HE: 3, Molotov: 4, Incendiary: 5, Decoy: 6},
			UnusedUtilityValue:           700,
		},
	}
}

func TestJSONExportsUtilityStatsAndExistingFlashAssists(t *testing.T) {
	t.Chdir(t.TempDir())
	want := utilityPlayer()

	fileName, err := WriteAnalysisToFile(analysisWith(want), "json")
	if err != nil {
		t.Fatalf("writing JSON: %v", err)
	}
	data, err := os.ReadFile(fileName)
	if err != nil {
		t.Fatalf("reading JSON: %v", err)
	}
	var analysis demoparser.ProcessedDemo
	if err := json.Unmarshal(data, &analysis); err != nil {
		t.Fatalf("unmarshalling JSON: %v", err)
	}
	got := analysis.Players[want.SteamID]
	if got == nil {
		t.Fatal("exported player missing")
	}
	if got.AssistStats.FlashedEnemies != 2 {
		t.Errorf("flash assists = %d, want 2", got.AssistStats.FlashedEnemies)
	}
	if !reflect.DeepEqual(got.UtilityStats, want.UtilityStats) {
		t.Errorf("utility stats = %+v, want %+v", got.UtilityStats, want.UtilityStats)
	}
}

func TestCSVAppendsStableUtilityAndRatingColumns(t *testing.T) {
	t.Chdir(t.TempDir())
	player := utilityPlayer()
	player.Rating = demoparser.RatingStats{
		Value: 1.234, Kills: 1.1, Damage: 0.9, Survival: 1, KAST: 1.05, MultiKill: 0.5, RoundSwing: 1.2,
	}
	player.PlayerMapStats.ApproxEKASTPercent = 104.3
	player.PlayerMapStats.ApproxRoundSwingPercent = -4.5

	fileName, err := WriteAnalysisToFile(analysisWith(player), "csv")
	if err != nil {
		t.Fatalf("writing CSV: %v", err)
	}
	f, err := os.Open(fileName)
	if err != nil {
		t.Fatalf("opening CSV: %v", err)
	}
	defer f.Close()
	records, err := csv.NewReader(f).ReadAll()
	if err != nil {
		t.Fatalf("reading CSV: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("CSV records = %d, want header and one player", len(records))
	}

	wantHeader := slices.Concat(legacyCSVHeader, utilityCSVHeader, ratingCSVHeader, approxCSVHeader)
	if !reflect.DeepEqual(records[0], wantHeader) {
		t.Errorf("CSV header = %q, want %q", records[0], wantHeader)
	}
	wantUtilityValues := []string{"3", "4", "5.2", "1.8", "60", "40", "20", "21", "1", "2", "3", "4", "5", "6", "700"}
	utilityValues := records[1][len(legacyCSVHeader) : len(legacyCSVHeader)+len(utilityCSVHeader)]
	if !reflect.DeepEqual(utilityValues, wantUtilityValues) {
		t.Errorf("CSV utility values = %q, want %q", utilityValues, wantUtilityValues)
	}
	wantRatingValues := []string{"1.23", "1.10", "0.90", "1.00", "1.05", "0.50", "1.20"}
	ratingStart := len(legacyCSVHeader) + len(utilityCSVHeader)
	if got := records[1][ratingStart : ratingStart+len(ratingCSVHeader)]; !reflect.DeepEqual(got, wantRatingValues) {
		t.Errorf("CSV rating values = %q, want %q", got, wantRatingValues)
	}
	wantApproxValues := []string{"104.3", "-4.5"}
	if got := records[1][ratingStart+len(ratingCSVHeader):]; !reflect.DeepEqual(got, wantApproxValues) {
		t.Errorf("CSV approx metric values = %q, want %q", got, wantApproxValues)
	}
}

func TestUtilityDetailTables(t *testing.T) {
	player := utilityPlayer()
	players := map[uint64]*demoparser.DemoPlayer{player.SteamID: player}
	sorted := sortedByKills(players)

	var effectiveness strings.Builder
	printUtilityEffectivenessTable(sorted, &effectiveness)
	for _, text := range []string{"Utility effectiveness", "Flash assists", "Enemies flashed", "Unused value", "utility-player"} {
		if !strings.Contains(strings.ToLower(effectiveness.String()), strings.ToLower(text)) {
			t.Errorf("utility effectiveness table missing %q:\n%s", text, effectiveness.String())
		}
	}

	var throws strings.Builder
	printGrenadesThrownTable(sorted, &throws)
	for _, text := range []string{"Grenades thrown", "Flash", "Smoke", "Incendiary", "Decoy", "utility-player"} {
		if !strings.Contains(strings.ToLower(throws.String()), strings.ToLower(text)) {
			t.Errorf("grenades thrown table missing %q:\n%s", text, throws.String())
		}
	}
}

func TestRatingDetailTable(t *testing.T) {
	player := utilityPlayer()
	player.Rating = demoparser.RatingStats{Value: 1.23, Kills: 1.1}
	sorted := sortedByKills(map[uint64]*demoparser.DemoPlayer{player.SteamID: player})

	var breakdown strings.Builder
	printRatingTable(sorted, &breakdown)
	for _, text := range []string{"Rating breakdown", "eKAST sub-rating", "Round swing sub-rating", "1.23", "utility-player"} {
		if !strings.Contains(strings.ToLower(breakdown.String()), strings.ToLower(text)) {
			t.Errorf("rating table missing %q:\n%s", text, breakdown.String())
		}
	}
}

// TestApproxMetricsDetailTable checks the compact derived-metrics table: both
// approximate labels, one-decimal eKAST, and explicitly signed swing.
func TestApproxMetricsDetailTable(t *testing.T) {
	up := utilityPlayer()
	up.PlayerMapStats.ApproxEKASTPercent = 104.3
	up.PlayerMapStats.ApproxRoundSwingPercent = 4
	down := &demoparser.DemoPlayer{
		SteamID: 2,
		Name:    "down-player",
		PlayerMapStats: demoparser.PlayerMapStats{
			ApproxEKASTPercent:      51.9,
			ApproxRoundSwingPercent: -4,
		},
	}
	sorted := sortedByKills(map[uint64]*demoparser.DemoPlayer{up.SteamID: up, down.SteamID: down})

	var out strings.Builder
	printApproxMetricsTable(sorted, &out)
	rendered := strings.ToLower(out.String())
	for _, text := range []string{"Approximate Rating 3.0 metrics", "Approx. eKAST (%)", "Approx. swing (%)", "104.3", "+4.0", "51.9", "-4.0", "utility-player", "down-player"} {
		if !strings.Contains(rendered, strings.ToLower(text)) {
			t.Errorf("approx metrics table missing %q:\n%s", text, out.String())
		}
	}
}

// TestJSONExportsApproxMetricsAndPreservesRatingKeys pins the exact new JSON
// keys and the normalized rating keys they must not replace.
func TestJSONExportsApproxMetricsAndPreservesRatingKeys(t *testing.T) {
	t.Chdir(t.TempDir())
	player := utilityPlayer()
	player.PlayerMapStats = demoparser.PlayerMapStats{
		KAST:                    75,
		ApproxEKASTPercent:      104.3,
		ApproxRoundSwingPercent: -4.5,
	}
	player.Rating = demoparser.RatingStats{KAST: 1.32, RoundSwing: 0.89}

	fileName, err := WriteAnalysisToFile(analysisWith(player), "json")
	if err != nil {
		t.Fatalf("writing JSON: %v", err)
	}
	data, err := os.ReadFile(fileName)
	if err != nil {
		t.Fatalf("reading JSON: %v", err)
	}
	content := string(data)
	for _, want := range []string{
		`"approx_ekast_percent": 104.3`,
		`"approx_round_swing_percent": -4.5`,
		`"kast": 75`,
		`"kast": 1.32`,
		`"round_swing": 0.89`,
	} {
		if !strings.Contains(content, want) {
			t.Errorf("JSON export missing %s:\n%s", want, content)
		}
	}
}
