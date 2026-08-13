package dataexport

import (
	"encoding/csv"
	"encoding/json"
	"maps"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	demoparser "github.com/taua-almeida/cs2-analyser-tool/cmd/demo_parser"
)

// analysisWith wraps players into the analysis record the writer takes,
// standing in for the selected copy cmd/analyse.go builds.
func analysisWith(players ...*demoparser.DemoPlayer) *demoparser.ProcessedDemo {
	byID := make(map[uint64]*demoparser.DemoPlayer, len(players))
	for _, player := range players {
		byID[player.SteamID] = player
	}
	return &demoparser.ProcessedDemo{Players: byID}
}

// selectedAnalysis is a one-player analysis with full match-level data, as
// the writer receives it after player selection. The teams keep describing
// the whole match: selection narrows players only, so the roster references
// a SteamID that is not among the selected players.
func selectedAnalysis() *demoparser.ProcessedDemo {
	selected := utilityPlayer()
	selected.SteamID = 76561198000000000
	selected.TeamID = 1
	analysis := analysisWith(selected)
	analysis.Teams = []demoparser.DemoTeam{
		{TeamID: 1, Name: "AlphaSquad", Aliases: []string{"AlphaSquad"}, Score: 11, Roster: []uint64{76561198000000000}},
		{TeamID: 2, Name: "BravoCrew", Aliases: []string{"BravoCrew", "BravoCrew GG"}, Score: 13, Roster: []uint64{76561198000000001}},
	}
	analysis.Map = demoparser.MapData{
		MapName:     "de_mirage",
		TotalRounds: 24,
		RoundsWonCT: 11,
		RoundsWonT:  13,
	}
	analysis.GameMode = "premier"
	return analysis
}

// writeToTempAndRead runs a save-file writer inside a fresh temp working
// directory and returns the produced file's bytes.
func writeToTempAndRead(t *testing.T, write func() (string, error)) []byte {
	t.Helper()
	t.Chdir(t.TempDir())
	fileName, err := write()
	if err != nil {
		t.Fatalf("writing: %v", err)
	}
	data, err := os.ReadFile(fileName)
	if err != nil {
		t.Fatalf("reading %s: %v", fileName, err)
	}
	return data
}

func writeAndRead(t *testing.T, analysis *demoparser.ProcessedDemo, saveType string) []byte {
	t.Helper()
	return writeToTempAndRead(t, func() (string, error) {
		return WriteAnalysisToFile(analysis, saveType)
	})
}

// TestJSONEnvelopeTopLevelShape pins the saved document contract: exactly
// the four envelope keys, never the previous bare SteamID-keyed player map.
func TestJSONEnvelopeTopLevelShape(t *testing.T) {
	data := writeAndRead(t, selectedAnalysis(), "json")

	var topLevel map[string]json.RawMessage
	if err := json.Unmarshal(data, &topLevel); err != nil {
		t.Fatalf("unmarshalling top level: %v", err)
	}
	gotKeys := slices.Sorted(maps.Keys(topLevel))
	wantKeys := []string{"game_mode", "map_data", "players", "teams"}
	if !reflect.DeepEqual(gotKeys, wantKeys) {
		t.Fatalf("top-level keys = %q, want %q", gotKeys, wantKeys)
	}

	var playersByID map[string]json.RawMessage
	if err := json.Unmarshal(topLevel["players"], &playersByID); err != nil {
		t.Fatalf("unmarshalling players object: %v", err)
	}
	if _, ok := playersByID["76561198000000000"]; !ok || len(playersByID) != 1 {
		t.Errorf("players object keys = %v, want only the selected SteamID string", slices.Sorted(maps.Keys(playersByID)))
	}
}

// TestJSONEnvelopeDecodesAsProcessedDemo round-trips the selected player and
// match-level data through the shared struct.
func TestJSONEnvelopeDecodesAsProcessedDemo(t *testing.T) {
	analysis := selectedAnalysis()
	data := writeAndRead(t, analysis, "json")

	var got demoparser.ProcessedDemo
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshalling as ProcessedDemo: %v", err)
	}
	want := analysis.Players[76561198000000000]
	gotPlayer := got.Players[want.SteamID]
	if gotPlayer == nil {
		t.Fatal("selected player missing from players")
	}
	if !reflect.DeepEqual(gotPlayer, want) {
		t.Errorf("selected player = %+v, want %+v", gotPlayer, want)
	}
	if len(got.Players) != 1 {
		t.Errorf("players = %d entries, want only the selected player", len(got.Players))
	}
	if !reflect.DeepEqual(got.Map, analysis.Map) {
		t.Errorf("map_data = %+v, want %+v", got.Map, analysis.Map)
	}
	if got.GameMode != "premier" {
		t.Errorf("game_mode = %q, want %q", got.GameMode, "premier")
	}
	if !reflect.DeepEqual(got.Teams, analysis.Teams) {
		t.Errorf("teams = %+v, want %+v", got.Teams, analysis.Teams)
	}
	if gotPlayer.TeamID != 1 {
		t.Errorf("selected player team_id = %d, want 1", gotPlayer.TeamID)
	}
}

// TestJSONEnvelopeKeepsEmptyGameMode pins that an empty mode is written as "".
func TestJSONEnvelopeKeepsEmptyGameMode(t *testing.T) {
	analysis := selectedAnalysis()
	analysis.GameMode = ""
	data := writeAndRead(t, analysis, "json")

	var topLevel map[string]json.RawMessage
	if err := json.Unmarshal(data, &topLevel); err != nil {
		t.Fatalf("unmarshalling top level: %v", err)
	}
	raw, ok := topLevel["game_mode"]
	if !ok {
		t.Fatal("game_mode omitted, want it serialized as \"\"")
	}
	if string(raw) != `""` {
		t.Errorf("game_mode = %s, want \"\"", raw)
	}
}

// TestCSVIgnoresMatchLevelData pins that the envelope changed nothing for
// CSV: one row per selected player and no map or game-mode values anywhere.
func TestCSVIgnoresMatchLevelData(t *testing.T) {
	data := writeAndRead(t, selectedAnalysis(), "csv")

	records, err := csv.NewReader(strings.NewReader(string(data))).ReadAll()
	if err != nil {
		t.Fatalf("reading CSV: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("CSV records = %d, want header and the one selected player", len(records))
	}
	if records[0][0] != "Name" || records[1][0] != "utility-player" {
		t.Errorf("CSV first column = %q/%q, want Name/utility-player", records[0][0], records[1][0])
	}
	for _, matchValue := range []string{"de_mirage", "premier", "map_data", "game_mode", "teams", "team_id", "AlphaSquad", "BravoCrew"} {
		if strings.Contains(string(data), matchValue) {
			t.Errorf("CSV contains match-level value %q, want players only", matchValue)
		}
	}
}
