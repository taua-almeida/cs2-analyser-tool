package dataexport

import (
	"encoding/csv"
	"encoding/json"
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"

	demoparser "github.com/taua-almeida/cs2-analyser-tool/cmd/demo_parser"
)

// seriesFixture is a hand-built completed BO3 for export tests: two series
// teams, one aggregate player per team — "sharp" with a recomputed rating,
// "quiet" with the rating explicitly omitted — and two maps whose analyses
// carry the standalone envelope.
func seriesFixture() *demoparser.ProcessedSeries {
	rating := demoparser.RatingStats{Value: 1.23, Kills: 1.1, Damage: 0.9, Survival: 1, KAST: 1.05, MultiKill: 0.5, RoundSwing: 1.2}
	sharp := &demoparser.SeriesPlayer{
		SteamID: 76561198000000001, Name: "sharp", Aliases: []string{"sharp"},
		TeamID: 1, MapsPlayed: 2, Rounds: 45, Deaths: 25,
		KillStats:   demoparser.KillStats{Total: 40, HeadShots: 22, Precision: 0.55, WeaponsKills: map[string]int{"AK-47": 30}},
		AssistStats: demoparser.AssistStats{Total: 9, DamageGiven: 3600, ADR: 80},
		PlayerStats: demoparser.PlayerMapStats{KAST: 71.1},
		Rating:      &rating,
	}
	quiet := &demoparser.SeriesPlayer{
		SteamID: 76561198000000002, Name: "quiet", Aliases: []string{"quiet"},
		TeamID: 2, MapsPlayed: 2, Rounds: 45, Deaths: 30,
		KillStats:   demoparser.KillStats{Total: 12, HeadShots: 4, WeaponsKills: map[string]int{"M4A1": 8}},
		AssistStats: demoparser.AssistStats{Total: 14, DamageGiven: 1500, ADR: 33.3},
	}
	mapAnalysis := func(name string, scoreOne, scoreTwo int) *demoparser.ProcessedDemo {
		return &demoparser.ProcessedDemo{
			Players: map[uint64]*demoparser.DemoPlayer{},
			Teams: []demoparser.DemoTeam{
				{TeamID: 1, Name: "RedSquad", Aliases: []string{"RedSquad"}, Score: scoreOne, Roster: []uint64{sharp.SteamID}},
				{TeamID: 2, Name: "BlueCrew", Aliases: []string{"BlueCrew"}, Score: scoreTwo, Roster: []uint64{quiet.SteamID}},
			},
			Map:      demoparser.MapData{MapName: name, TotalRounds: scoreOne + scoreTwo},
			GameMode: "competitive",
		}
	}
	return &demoparser.ProcessedSeries{
		BestOf:       3,
		WinnerTeamID: 1,
		Teams: []demoparser.SeriesTeam{
			{TeamID: 1, Name: "RedSquad", Aliases: []string{"RedSquad"}, MapsWon: 2, RoundsWon: 26, Roster: []uint64{sharp.SteamID}},
			{TeamID: 2, Name: "BlueCrew", Aliases: []string{"BlueCrew"}, MapsWon: 0, RoundsWon: 19, Roster: []uint64{quiet.SteamID}},
		},
		Players: map[uint64]*demoparser.SeriesPlayer{sharp.SteamID: sharp, quiet.SteamID: quiet},
		Maps: []demoparser.SeriesMap{
			{
				SHA256:       "digest-one",
				WinnerTeamID: 1,
				TeamAssignments: []demoparser.SeriesTeamAssignment{
					{MapTeamID: 1, SeriesTeamID: 1}, {MapTeamID: 2, SeriesTeamID: 2},
				},
				Analysis: mapAnalysis("de_first", 13, 9),
			},
			{
				SHA256:       "digest-two",
				WinnerTeamID: 1,
				TeamAssignments: []demoparser.SeriesTeamAssignment{
					{MapTeamID: 1, SeriesTeamID: 1}, {MapTeamID: 2, SeriesTeamID: 2},
				},
				Analysis: mapAnalysis("de_second", 13, 10),
			},
		},
	}
}

// TestSeriesCSVKeepsFlatPlayerContract pins the series CSV to the existing
// single-map contract: identical header and per-player formatting, and no
// series, team, hash or map metadata anywhere in the file.
func TestSeriesCSVKeepsFlatPlayerContract(t *testing.T) {
	data := writeToTempAndRead(t, func() (string, error) {
		return WriteSeriesToFile(seriesFixture(), "csv")
	})
	records, err := csv.NewReader(strings.NewReader(string(data))).ReadAll()
	if err != nil {
		t.Fatalf("parsing series CSV: %v", err)
	}
	if len(records) != 3 {
		t.Fatalf("CSV records = %d, want header plus the two aggregate players", len(records))
	}

	wantHeader := slices.Concat(legacyCSVHeader, utilityCSVHeader, ratingCSVHeader, approxCSVHeader)
	if !reflect.DeepEqual(records[0], wantHeader) {
		t.Errorf("series CSV header = %q, want the unchanged flat contract %q", records[0], wantHeader)
	}
	if records[1][0] != "sharp" || records[2][0] != "quiet" {
		t.Errorf("series CSV rows = %q/%q, want most kills first", records[1][0], records[2][0])
	}
	ratingStart := len(legacyCSVHeader) + len(utilityCSVHeader)
	wantSharpRating := []string{"1.23", "1.10", "0.90", "1.00", "1.05", "0.50", "1.20"}
	if got := records[1][ratingStart : ratingStart+len(ratingCSVHeader)]; !reflect.DeepEqual(got, wantSharpRating) {
		t.Errorf("recomputed rating cells = %q, want %q", got, wantSharpRating)
	}
	wantOmittedRating := make([]string, len(ratingCSVHeader))
	if got := records[2][ratingStart : ratingStart+len(ratingCSVHeader)]; !reflect.DeepEqual(got, wantOmittedRating) {
		t.Errorf("omitted rating cells = %q, want all empty; an absent rating must not print as 0.00", got)
	}
	for _, leaked := range []string{"digest-one", "digest-two", "de_first", "de_second", "RedSquad", "BlueCrew", "best_of", "team_id", "sha256", "maps_won"} {
		if strings.Contains(string(data), leaked) {
			t.Errorf("series CSV contains %q; series metadata must stay JSON-only", leaked)
		}
	}
}

// TestSeriesJSONEnvelope pins the saved series document: the five envelope
// keys, ordered maps with digests and assignments, standalone analyses
// inside each map, no user_id in aggregate players, and an explicit null
// rating when it was not recomputed.
func TestSeriesJSONEnvelope(t *testing.T) {
	series := seriesFixture()
	data := writeToTempAndRead(t, func() (string, error) {
		return WriteSeriesToFile(series, "json")
	})

	var topLevel map[string]json.RawMessage
	if err := json.Unmarshal(data, &topLevel); err != nil {
		t.Fatalf("unmarshalling top level: %v", err)
	}
	gotKeys := slices.Sorted(maps.Keys(topLevel))
	wantKeys := []string{"best_of", "maps", "players", "teams", "winner_team_id"}
	if !reflect.DeepEqual(gotKeys, wantKeys) {
		t.Fatalf("top-level keys = %q, want %q", gotKeys, wantKeys)
	}

	var seriesMaps []map[string]json.RawMessage
	if err := json.Unmarshal(topLevel["maps"], &seriesMaps); err != nil {
		t.Fatalf("unmarshalling maps: %v", err)
	}
	if len(seriesMaps) != 2 {
		t.Fatalf("maps = %d entries, want 2", len(seriesMaps))
	}
	for i, want := range []string{`"digest-one"`, `"digest-two"`} {
		if got := string(seriesMaps[i]["sha256"]); got != want {
			t.Errorf("maps[%d].sha256 = %s, want %s; supplied order must be kept", i, got, want)
		}
		var analysis map[string]json.RawMessage
		if err := json.Unmarshal(seriesMaps[i]["analysis"], &analysis); err != nil {
			t.Fatalf("unmarshalling maps[%d].analysis: %v", i, err)
		}
		gotAnalysisKeys := slices.Sorted(maps.Keys(analysis))
		wantAnalysisKeys := []string{"game_mode", "map_data", "players", "teams"}
		if !reflect.DeepEqual(gotAnalysisKeys, wantAnalysisKeys) {
			t.Errorf("maps[%d].analysis keys = %q, want the standalone envelope %q", i, gotAnalysisKeys, wantAnalysisKeys)
		}
		if _, ok := seriesMaps[i]["team_assignments"]; !ok {
			t.Errorf("maps[%d] has no team_assignments", i)
		}
	}

	var playersByID map[string]map[string]json.RawMessage
	if err := json.Unmarshal(topLevel["players"], &playersByID); err != nil {
		t.Fatalf("unmarshalling players: %v", err)
	}
	quiet := playersByID["76561198000000002"]
	if quiet == nil {
		t.Fatal("aggregate player 76561198000000002 missing")
	}
	if _, hasUserID := quiet["user_id"]; hasUserID {
		t.Error("aggregate player carries a map-local user_id; the series player type must not")
	}
	if got := string(quiet["rating"]); got != "null" {
		t.Errorf("omitted rating = %s, want an explicit null", got)
	}
	if got := string(quiet["maps_played"]); got != "2" {
		t.Errorf("maps_played = %s, want 2", got)
	}

	var roundTrip demoparser.ProcessedSeries
	if err := json.Unmarshal(data, &roundTrip); err != nil {
		t.Fatalf("round-tripping series JSON: %v", err)
	}
	if !reflect.DeepEqual(&roundTrip, series) {
		t.Error("series JSON does not round-trip through ProcessedSeries")
	}
}

// TestSeriesTables renders the series result and aggregate player tables
// and pins their load-bearing content: ordered maps, team labels, winner,
// the Maps column, the recomputed rating, and that a nil rating is not
// printed as a zero rating.
func TestSeriesTables(t *testing.T) {
	series := seriesFixture()

	// Comparisons are case-insensitive because go-pretty upper-cases header
	// cells when rendering.
	var result strings.Builder
	printSeriesResultTable(series, &result)
	rendered := strings.ToLower(result.String())
	for _, want := range []string{"Best-of-3", "de_first", "de_second", "RedSquad", "BlueCrew", "Series winner: RedSquad (2:0 maps, 26:19 rounds)"} {
		if !strings.Contains(rendered, strings.ToLower(want)) {
			t.Errorf("series result table missing %q:\n%s", want, result.String())
		}
	}

	var playersTable strings.Builder
	printSeriesPlayersTable(series, nil, &playersTable)
	rendered = strings.ToLower(playersTable.String())
	for _, want := range []string{"Maps", "sharp", "quiet", "1.23"} {
		if !strings.Contains(rendered, strings.ToLower(want)) {
			t.Errorf("series players table missing %q:\n%s", want, playersTable.String())
		}
	}
	if strings.Contains(rendered, "0.00") {
		t.Errorf("series players table renders an omitted rating as 0.00:\n%s", rendered)
	}

	var filtered strings.Builder
	printSeriesPlayersTable(series, map[uint64]bool{76561198000000001: true}, &filtered)
	if strings.Contains(filtered.String(), "quiet") {
		t.Errorf("player selection did not narrow the table:\n%s", filtered.String())
	}
}

// TestSeriesRatingTablePreservesOmission pins the aggregate rating
// breakdown: a recomputed rating prints normally while an omitted one shows
// "-" cells rather than a fabricated 0.00 rating.
func TestSeriesRatingTablePreservesOmission(t *testing.T) {
	series := seriesFixture()
	var breakdown strings.Builder
	printSeriesRatingTable(sortedSeriesByKills(series.Players), &breakdown)
	rendered := strings.ToLower(breakdown.String())
	for _, want := range []string{"Rating breakdown", "eKAST sub-rating", "sharp", "1.23", "quiet"} {
		if !strings.Contains(rendered, strings.ToLower(want)) {
			t.Errorf("series rating table missing %q:\n%s", want, breakdown.String())
		}
	}
	if strings.Contains(rendered, "0.00") {
		t.Errorf("series rating table renders an omitted rating as 0.00:\n%s", breakdown.String())
	}
}

// TestSeriesTeamLabelCollisions pins that two series teams sharing one clan
// name — which identity resolution permits, names being labels — render
// with their series-team IDs appended, so every column and winner stays
// attributable.
func TestSeriesTeamLabelCollisions(t *testing.T) {
	series := seriesFixture()
	series.Teams[0].Name = "SameName"
	series.Teams[1].Name = "SameName"
	var result strings.Builder
	printSeriesResultTable(series, &result)
	rendered := strings.ToLower(result.String())
	for _, want := range []string{"SameName (team 1)", "SameName (team 2)", "Series winner: SameName (team 1)"} {
		if !strings.Contains(rendered, strings.ToLower(want)) {
			t.Errorf("colliding-label result table missing %q:\n%s", want, result.String())
		}
	}

	// A real clan name can also collide with the other team's generated
	// fallback, so collisions must be detected on the final labels.
	fallback := seriesFixture()
	fallback.Teams[0].Name = ""
	fallback.Teams[1].Name = "Team 1"
	var fallbackResult strings.Builder
	printSeriesResultTable(fallback, &fallbackResult)
	rendered = strings.ToLower(fallbackResult.String())
	for _, want := range []string{"Team 1 (team 1)", "Team 1 (team 2)", "Series winner: Team 1 (team 1)"} {
		if !strings.Contains(rendered, strings.ToLower(want)) {
			t.Errorf("fallback-collision result table missing %q:\n%s", want, fallbackResult.String())
		}
	}
}

// TestSortedSeriesByKillsTieBreaksBySteamID pins the final ordering key:
// two players sharing a kill count and display name must not inherit the
// map's random iteration order.
func TestSortedSeriesByKillsTieBreaksBySteamID(t *testing.T) {
	players := map[uint64]*demoparser.SeriesPlayer{
		11: {SteamID: 11, Name: "twin", KillStats: demoparser.KillStats{Total: 7}},
		5:  {SteamID: 5, Name: "twin", KillStats: demoparser.KillStats{Total: 7}},
	}
	sorted := sortedSeriesByKills(players)
	if sorted[0].SteamID != 5 || sorted[1].SteamID != 11 {
		t.Errorf("tied players sorted as %d, %d; want ascending SteamID 5, 11", sorted[0].SteamID, sorted[1].SteamID)
	}
}
