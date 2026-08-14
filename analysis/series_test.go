package analysis

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// testTeam describes one logical team of a synthetic series map.
type testTeam struct {
	id     int
	name   string
	roster []uint64
	score  int
}

// rosterA and rosterB are the two default lineups of the synthetic series.
var (
	rosterA = []uint64{1, 2, 3, 4, 5}
	rosterB = []uint64{6, 7, 8, 9, 10}
)

// testMapDemo builds a minimal parsed map: the given logical teams, one
// player per rostered SteamID named "player-<id>", and a round total equal
// to the score sum. It deliberately has no aggFacts, like any MapAnalysis
// a test constructs by hand.
func testMapDemo(mapName string, teams ...testTeam) *MapAnalysis {
	demo := &MapAnalysis{
		Players: map[uint64]*DemoPlayer{},
		Map:     MapData{MapName: mapName},
	}
	for _, team := range teams {
		aliases := []string{}
		if team.name != "" {
			aliases = []string{team.name}
		}
		demo.Teams = append(demo.Teams, DemoTeam{
			TeamID:  team.id,
			Name:    team.name,
			Aliases: aliases,
			Score:   team.score,
			Roster:  sortedIDs(team.roster),
		})
		demo.Map.TotalRounds += team.score
		for _, id := range team.roster {
			demo.Players[id] = &DemoPlayer{
				SteamID:   id,
				Name:      fmt.Sprintf("player-%d", id),
				TeamID:    team.id,
				KillStats: KillStats{WeaponsKills: map[string]int{}},
			}
		}
	}
	return demo
}

// abMap is a standard map between the two default lineups: team 1 is A,
// team 2 is B, named after their lineup.
func abMap(mapName string, scoreA, scoreB int) *MapAnalysis {
	return testMapDemo(mapName,
		testTeam{id: 1, name: "Alpha", roster: rosterA, score: scoreA},
		testTeam{id: 2, name: "Bravo", roster: rosterB, score: scoreB})
}

func seriesInputs(demos ...*MapAnalysis) []SeriesMapInput {
	inputs := make([]SeriesMapInput, len(demos))
	for i, demo := range demos {
		inputs[i] = SeriesMapInput{Demo: demo, SHA256: fmt.Sprintf("digest-%d", i+1)}
	}
	return inputs
}

func TestBuildSeriesCompletedPaths(t *testing.T) {
	tests := []struct {
		name       string
		bestOf     int
		scores     [][2]int // per map: rounds of lineup A and lineup B
		wantWinner int      // series team ID: 1 is lineup A, 2 is lineup B
		wantMaps   [2]int
		wantRounds [2]int
	}{
		{name: "bo3 2-0", bestOf: 3, scores: [][2]int{{13, 9}, {13, 11}}, wantWinner: 1, wantMaps: [2]int{2, 0}, wantRounds: [2]int{26, 20}},
		{name: "bo3 2-1", bestOf: 3, scores: [][2]int{{13, 9}, {7, 13}, {10, 13}}, wantWinner: 2, wantMaps: [2]int{1, 2}, wantRounds: [2]int{30, 35}},
		{name: "bo5 3-0", bestOf: 5, scores: [][2]int{{13, 9}, {13, 10}, {13, 11}}, wantWinner: 1, wantMaps: [2]int{3, 0}, wantRounds: [2]int{39, 30}},
		{name: "bo5 3-1", bestOf: 5, scores: [][2]int{{13, 9}, {9, 13}, {13, 10}, {13, 11}}, wantWinner: 1, wantMaps: [2]int{3, 1}, wantRounds: [2]int{48, 43}},
		{name: "bo5 3-2", bestOf: 5, scores: [][2]int{{13, 9}, {9, 13}, {13, 10}, {11, 13}, {16, 13}}, wantWinner: 1, wantMaps: [2]int{3, 2}, wantRounds: [2]int{62, 58}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			demos := make([]*MapAnalysis, len(test.scores))
			for i, score := range test.scores {
				demos[i] = abMap(fmt.Sprintf("de_map%d", i+1), score[0], score[1])
			}
			series, err := BuildSeries(test.bestOf, seriesInputs(demos...))
			if err != nil {
				t.Fatalf("BuildSeries: %v", err)
			}
			if series.BestOf != test.bestOf {
				t.Errorf("best_of = %d, want %d", series.BestOf, test.bestOf)
			}
			if series.WinnerTeamID != test.wantWinner {
				t.Errorf("winner = %d, want %d", series.WinnerTeamID, test.wantWinner)
			}
			for i, team := range series.Teams {
				if team.TeamID != i+1 {
					t.Errorf("teams[%d].TeamID = %d, want %d", i, team.TeamID, i+1)
				}
				if team.MapsWon != test.wantMaps[i] {
					t.Errorf("team %d maps won = %d, want %d", team.TeamID, team.MapsWon, test.wantMaps[i])
				}
				if team.RoundsWon != test.wantRounds[i] {
					t.Errorf("team %d rounds won = %d, want %d", team.TeamID, team.RoundsWon, test.wantRounds[i])
				}
			}
			if got := series.Teams[0].Roster; !slices.Equal(got, rosterA) {
				t.Errorf("team 1 roster = %v, want %v", got, rosterA)
			}
			if got := series.Teams[1].Roster; !slices.Equal(got, rosterB) {
				t.Errorf("team 2 roster = %v, want %v", got, rosterB)
			}
			for i, seriesMap := range series.Maps {
				if seriesMap.SHA256 != fmt.Sprintf("digest-%d", i+1) {
					t.Errorf("maps[%d].sha256 = %q, want digest-%d", i, seriesMap.SHA256, i+1)
				}
				if got, want := seriesMap.Analysis.Map.MapName, fmt.Sprintf("de_map%d", i+1); got != want {
					t.Errorf("maps[%d] analysis = %q, want %q; map order must be preserved", i, got, want)
				}
				wantMapWinner := 1
				if test.scores[i][1] > test.scores[i][0] {
					wantMapWinner = 2
				}
				if seriesMap.WinnerTeamID != wantMapWinner {
					t.Errorf("maps[%d].winner_team_id = %d, want %d", i, seriesMap.WinnerTeamID, wantMapWinner)
				}
			}
		})
	}
}

func TestBuildSeriesRejectsInvalidFormats(t *testing.T) {
	m1, m2 := abMap("de_one", 13, 9), abMap("de_two", 13, 10)
	tests := []struct {
		name    string
		bestOf  int
		inputs  []SeriesMapInput
		wantErr string
	}{
		{name: "best-of 4", bestOf: 4, inputs: seriesInputs(m1, m2), wantErr: "invalid best-of 4"},
		{name: "bo3 one map", bestOf: 3, inputs: seriesInputs(m1), wantErr: "2 or 3 maps, got 1"},
		{name: "bo3 four maps", bestOf: 3, inputs: seriesInputs(m1, m2, abMap("de_three", 13, 9), abMap("de_four", 13, 9)), wantErr: "2 or 3 maps, got 4"},
		{name: "bo5 two maps", bestOf: 5, inputs: seriesInputs(m1, m2), wantErr: "3, 4 or 5 maps, got 2"},
		{name: "no maps", bestOf: 3, inputs: nil, wantErr: "2 or 3 maps, got 0"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := BuildSeries(test.bestOf, test.inputs)
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("BuildSeries error = %v, want it to contain %q", err, test.wantErr)
			}
		})
	}
}

func TestBuildSeriesRejectsIncompleteSeries(t *testing.T) {
	_, err := BuildSeries(3, seriesInputs(abMap("de_one", 13, 9), abMap("de_two", 9, 13)))
	if err == nil || !strings.Contains(err.Error(), "not complete") {
		t.Fatalf("BuildSeries error = %v, want incomplete-series rejection", err)
	}
	_, err = BuildSeries(5, seriesInputs(abMap("de_one", 13, 9), abMap("de_two", 9, 13), abMap("de_three", 13, 9)))
	if err == nil || !strings.Contains(err.Error(), "not complete") {
		t.Fatalf("BuildSeries error = %v, want incomplete-series rejection", err)
	}
}

func TestBuildSeriesRejectsMapsAfterClinch(t *testing.T) {
	_, err := BuildSeries(3, seriesInputs(abMap("de_one", 13, 9), abMap("de_two", 13, 10), abMap("de_three", 9, 13)))
	if err == nil || !strings.Contains(err.Error(), "after the clinch") {
		t.Fatalf("BuildSeries error = %v, want after-clinch rejection", err)
	}
	_, err = BuildSeries(5, seriesInputs(
		abMap("de_one", 13, 9), abMap("de_two", 13, 10), abMap("de_three", 13, 11), abMap("de_four", 9, 13)))
	if err == nil || !strings.Contains(err.Error(), "after the clinch") {
		t.Fatalf("BuildSeries error = %v, want after-clinch rejection", err)
	}
}

func TestBuildSeriesRejectsDuplicateContent(t *testing.T) {
	inputs := seriesInputs(abMap("de_one", 13, 9), abMap("de_two", 13, 10))
	inputs[1].SHA256 = inputs[0].SHA256
	_, err := BuildSeries(3, inputs)
	if err == nil || !strings.Contains(err.Error(), "same demo") {
		t.Fatalf("BuildSeries error = %v, want duplicate-content rejection", err)
	}
}

func TestBuildSeriesRejectsInvalidMaps(t *testing.T) {
	oneTeam := testMapDemo("de_solo", testTeam{id: 1, name: "Alpha", roster: rosterA, score: 13})
	tied := abMap("de_tied", 12, 12)
	badRounds := abMap("de_short", 13, 9)
	badRounds.Map.TotalRounds = 30
	emptyRoster := abMap("de_empty", 13, 9)
	emptyRoster.Teams[1].Roster = nil
	// A completed Wingman map passes every consistency check — nonempty
	// rosters, untied scores summing to the rounds — and is rejected only
	// by the 5v5 shape rule.
	wingman := testMapDemo("de_wingman",
		testTeam{id: 1, name: "Duo", roster: []uint64{1, 2}, score: 9},
		testTeam{id: 2, name: "Pair", roster: []uint64{6, 7}, score: 5})
	unknownTeamRef := abMap("de_unknown", 13, 9)
	unknownTeamRef.Players[1].TeamID = 7
	noDigest := seriesInputs(abMap("de_one", 13, 9), abMap("de_two", 13, 10))
	noDigest[1].SHA256 = ""
	// A casual demo can field two stable 5v5-sized lineups, so the named
	// mode alone must reject it.
	casual := abMap("de_casual", 13, 9)
	casual.GameMode = "casual"
	// An unknown-mode 10v10 passes the mode gate and the roster lower
	// bound; only the per-round participation ceiling catches it.
	crowdedA, crowdedB := make([]uint64, 10), make([]uint64, 10)
	for i := range crowdedA {
		crowdedA[i], crowdedB[i] = uint64(31+i), uint64(41+i)
	}
	crowded := testMapDemo("de_crowded",
		testTeam{id: 1, name: "Herd", roster: crowdedA, score: 13},
		testTeam{id: 2, name: "Flock", roster: crowdedB, score: 9})
	for _, player := range crowded.Players {
		player.SideStats.Rounds.Total = crowded.Map.TotalRounds
	}

	closer := abMap("de_two", 13, 10)
	tests := []struct {
		name    string
		inputs  []SeriesMapInput
		wantErr string
	}{
		{name: "one logical team", inputs: seriesInputs(oneTeam, closer), wantErr: "1 logical teams"},
		{name: "tied map", inputs: seriesInputs(tied, closer), wantErr: "tied 12:12"},
		{name: "scores disagree with rounds", inputs: seriesInputs(badRounds, closer), wantErr: "do not add up"},
		{name: "empty roster", inputs: seriesInputs(emptyRoster, closer), wantErr: "only the competitive 5v5 format"},
		{name: "wingman map", inputs: seriesInputs(wingman, closer), wantErr: "only the competitive 5v5 format"},
		{name: "casual mode", inputs: seriesInputs(casual, closer), wantErr: `game mode "casual"`},
		{name: "oversized unknown-mode lobby", inputs: seriesInputs(crowded, closer), wantErr: "more than a 5v5 lobby"},
		{name: "unknown player team id", inputs: seriesInputs(unknownTeamRef, closer), wantErr: "unknown map team ID 7"},
		{name: "missing demo", inputs: []SeriesMapInput{{SHA256: "d1"}, {Demo: closer, SHA256: "d2"}}, wantErr: "no parsed demo"},
		{name: "missing digest", inputs: noDigest, wantErr: "no SHA-256 digest"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := BuildSeries(3, test.inputs)
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("BuildSeries error = %v, want it to contain %q", err, test.wantErr)
			}
		})
	}
}

// TestBuildSeriesResolvesSwappedLocalIDs pins that map-local team numbering
// and side order carry no identity: map 2 lists lineup B first under team ID
// 1, and the assignment still crosses back through the rosters.
func TestBuildSeriesResolvesSwappedLocalIDs(t *testing.T) {
	m1 := abMap("de_one", 13, 9)
	m2 := testMapDemo("de_two",
		testTeam{id: 1, name: "Bravo", roster: rosterB, score: 11},
		testTeam{id: 2, name: "Alpha", roster: rosterA, score: 13})
	series, err := BuildSeries(3, seriesInputs(m1, m2))
	if err != nil {
		t.Fatalf("BuildSeries: %v", err)
	}
	if series.WinnerTeamID != 1 {
		t.Errorf("winner = %d, want lineup A as series team 1", series.WinnerTeamID)
	}
	wantAssignments := []SeriesTeamAssignment{
		{MapTeamID: 1, SeriesTeamID: 2},
		{MapTeamID: 2, SeriesTeamID: 1},
	}
	if got := series.Maps[1].TeamAssignments; !reflect.DeepEqual(got, wantAssignments) {
		t.Errorf("map 2 assignments = %+v, want %+v", got, wantAssignments)
	}
	if got := series.Teams[0].Roster; !slices.Equal(got, rosterA) {
		t.Errorf("team 1 roster = %v, want lineup A %v", got, rosterA)
	}
}

// TestBuildSeriesSubstitutionJoinsExistingTeam pins that a roster addition
// is a substitute on the same series team, not a new team.
func TestBuildSeriesSubstitutionJoinsExistingTeam(t *testing.T) {
	m1 := abMap("de_one", 13, 9)
	m2 := testMapDemo("de_two",
		testTeam{id: 1, name: "Alpha", roster: []uint64{1, 2, 3, 4, 11}, score: 13},
		testTeam{id: 2, name: "Bravo", roster: rosterB, score: 10})
	series, err := BuildSeries(3, seriesInputs(m1, m2))
	if err != nil {
		t.Fatalf("BuildSeries: %v", err)
	}
	wantRoster := []uint64{1, 2, 3, 4, 5, 11}
	if got := series.Teams[0].Roster; !slices.Equal(got, wantRoster) {
		t.Errorf("team 1 roster = %v, want the substitute unioned in: %v", got, wantRoster)
	}
	substitute := series.Players[11]
	if substitute == nil {
		t.Fatal("substitute missing from aggregate players")
	}
	if substitute.TeamID != 1 {
		t.Errorf("substitute team = %d, want 1", substitute.TeamID)
	}
	if substitute.MapsPlayed != 1 || substitute.Rounds != 23 {
		t.Errorf("substitute maps/rounds = %d/%d, want 1/23", substitute.MapsPlayed, substitute.Rounds)
	}
}

// TestBuildSeriesJointAssignmentBeatsGreedyMatching pins the joint
// evaluation requirement: map 2 is played entirely by two full squads of
// substitutes and shares no SteamID with map 1, so alone it is ambiguous;
// map 3 ties one substitute of each squad back to a lineup, which makes
// exactly one global assignment valid. A greedy map-by-map matcher would
// have failed on map 2.
func TestBuildSeriesJointAssignmentBeatsGreedyMatching(t *testing.T) {
	subsA := []uint64{21, 22, 23, 24, 25}
	subsB := []uint64{26, 27, 28, 29, 30}
	m1 := abMap("de_one", 13, 9)
	m2 := testMapDemo("de_two",
		testTeam{id: 1, name: "", roster: subsB, score: 13},
		testTeam{id: 2, name: "", roster: subsA, score: 11})
	m3 := testMapDemo("de_three",
		testTeam{id: 1, name: "Alpha", roster: []uint64{1, 2, 3, 4, 21}, score: 10},
		testTeam{id: 2, name: "Bravo", roster: []uint64{6, 7, 8, 9, 26}, score: 13})
	series, err := BuildSeries(3, seriesInputs(m1, m2, m3))
	if err != nil {
		t.Fatalf("BuildSeries: %v", err)
	}
	if series.WinnerTeamID != 2 {
		t.Errorf("winner = %d, want 2", series.WinnerTeamID)
	}
	if got, want := series.Teams[0].Roster, []uint64{1, 2, 3, 4, 5, 21, 22, 23, 24, 25}; !slices.Equal(got, want) {
		t.Errorf("team 1 roster = %v, want %v", got, want)
	}
	if got, want := series.Teams[1].Roster, []uint64{6, 7, 8, 9, 10, 26, 27, 28, 29, 30}; !slices.Equal(got, want) {
		t.Errorf("team 2 roster = %v, want %v", got, want)
	}
	wantAssignments := []SeriesTeamAssignment{
		{MapTeamID: 1, SeriesTeamID: 2},
		{MapTeamID: 2, SeriesTeamID: 1},
	}
	if got := series.Maps[1].TeamAssignments; !reflect.DeepEqual(got, wantAssignments) {
		t.Errorf("map 2 assignments = %+v, want %+v", got, wantAssignments)
	}
}

func TestBuildSeriesConflictIsStructuredError(t *testing.T) {
	m1 := abMap("de_one", 13, 9)
	// Map 2 mixes the lineups: each roster holds players from both of map
	// 1's teams, so every orientation places someone on both series teams.
	m2 := testMapDemo("de_two",
		testTeam{id: 1, name: "Mixed", roster: []uint64{1, 2, 3, 4, 6}, score: 13},
		testTeam{id: 2, name: "Rest", roster: []uint64{5, 7, 8, 9, 10}, score: 10})
	_, err := BuildSeries(3, seriesInputs(m1, m2))
	var conflict *SeriesTeamConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("BuildSeries error = %v, want a *SeriesTeamConflictError", err)
	}
	if len(conflict.Maps) != 2 || len(conflict.Candidates) != 2 {
		t.Fatalf("conflict carries %d maps and %d candidates, want 2 and 2", len(conflict.Maps), len(conflict.Candidates))
	}
	if conflict.Maps[1].MapIndex != 1 || conflict.Maps[1].TeamIDs != [2]int{1, 2} {
		t.Errorf("conflict map evidence = %+v, want map index 1 with team IDs [1 2]", conflict.Maps[1])
	}
	for i, candidate := range conflict.Candidates {
		if len(candidate.Conflicts) == 0 {
			t.Errorf("candidate %d has no conflicting SteamIDs; a conflict error requires all candidates to fail", i)
		}
		if len(candidate.Assignments) != 2 {
			t.Errorf("candidate %d covers %d maps, want 2", i, len(candidate.Assignments))
		}
	}
}

// TestBuildSeriesAmbiguityIsStructuredError pins two rules at once: fully
// disjoint rosters leave both orientations valid, and identical clan names
// on map 2 must not break the tie, because names are labels, not identity.
func TestBuildSeriesAmbiguityIsStructuredError(t *testing.T) {
	m1 := abMap("de_one", 13, 9)
	m2 := testMapDemo("de_two",
		testTeam{id: 1, name: "Alpha", roster: []uint64{11, 12, 13, 14, 15}, score: 13},
		testTeam{id: 2, name: "Bravo", roster: []uint64{16, 17, 18, 19, 20}, score: 10})
	_, err := BuildSeries(3, seriesInputs(m1, m2))
	var ambiguity *SeriesTeamAmbiguityError
	if !errors.As(err, &ambiguity) {
		t.Fatalf("BuildSeries error = %v, want a *SeriesTeamAmbiguityError", err)
	}
	if len(ambiguity.Candidates) != 2 {
		t.Fatalf("ambiguity carries %d candidates, want the 2 competing assignments", len(ambiguity.Candidates))
	}
	for i, candidate := range ambiguity.Candidates {
		if len(candidate.Conflicts) != 0 {
			t.Errorf("candidate %d has conflicts %v, want none for a valid candidate", i, candidate.Conflicts)
		}
	}
	if len(ambiguity.Maps) != 2 || !slices.Equal(ambiguity.Maps[1].Rosters[0], []uint64{11, 12, 13, 14, 15}) {
		t.Errorf("ambiguity map evidence = %+v, want map 2 rosters", ambiguity.Maps)
	}
}

// TestBuildSeriesClanNamesAreLabels pins alias behavior when a lineup is
// relabeled mid-series: identity still follows rosters, every observed name
// is kept in first-observation order, and the display name is the
// most-observed one with the first observation breaking ties.
func TestBuildSeriesClanNamesAreLabels(t *testing.T) {
	m1 := abMap("de_one", 13, 9)
	m2 := testMapDemo("de_two",
		testTeam{id: 1, name: "Alpha GG", roster: rosterA, score: 9},
		testTeam{id: 2, name: "Bravo", roster: rosterB, score: 13})
	m3 := testMapDemo("de_three",
		testTeam{id: 1, name: "Alpha GG", roster: rosterA, score: 7},
		testTeam{id: 2, name: "Bravo", roster: rosterB, score: 13})
	series, err := BuildSeries(3, seriesInputs(m1, m2, m3))
	if err != nil {
		t.Fatalf("BuildSeries: %v", err)
	}
	teamOne := series.Teams[0]
	if !slices.Equal(teamOne.Aliases, []string{"Alpha", "Alpha GG"}) {
		t.Errorf("team 1 aliases = %v, want first-observation order [Alpha, Alpha GG]", teamOne.Aliases)
	}
	if teamOne.Name != "Alpha GG" {
		t.Errorf("team 1 name = %q, want the most-observed %q", teamOne.Name, "Alpha GG")
	}
	if series.Teams[1].Name != "Bravo" {
		t.Errorf("team 2 name = %q, want %q", series.Teams[1].Name, "Bravo")
	}
}

func TestBuildSeriesAggregatesAdditiveStatsExactly(t *testing.T) {
	m1 := abMap("de_one", 13, 9)
	m2 := abMap("de_two", 13, 10)
	statsOne := &DemoPlayer{
		SteamID: 1, Name: "player-1", UserID: 3, TeamID: 1,
		Deaths:       12,
		DeathsTraded: SideCount{Total: 3, CT: 2, T: 1},
		KillStats: KillStats{
			Total: 20, HeadShots: 9, WeaponsKills: map[string]int{"AK-47": 12, "AWP": 8},
			TradeKills: 4, TeamKills: 1,
		},
		AssistStats: AssistStats{Total: 5, FlashedEnemies: 2, DamageGiven: 1800},
		PlayerMapStats: PlayerMapStats{
			MVPs: 3, ACEs: 1, MultiKills: MultiKillRounds{K2: 3, K3: 1, K5: 1}, ClutchesWon: 2,
		},
		OpeningDuelStats: OpeningDuelStats{
			OpeningKills:  SideCount{Total: 4, CT: 3, T: 1},
			OpeningDeaths: SideCount{Total: 2, CT: 1, T: 1},
		},
		SideStats: SideStats{
			Rounds: SideCount{Total: 22, CT: 12, T: 10},
			Kills:  SideCount{Total: 20, CT: 11, T: 9},
			Deaths: SideCount{Total: 12, CT: 5, T: 7},
		},
		UtilityStats: UtilityStats{
			EnemiesFlashed: 6, FriendsFlashed: 2, EnemyFlashTimeSeconds: 10.5,
			UtilityDamage:      UtilityDamageStats{Total: 90, HE: 60, Fire: 30},
			GrenadesThrown:     GrenadesThrownStats{Total: 15, Flash: 5, Smoke: 4, HE: 3, Molotov: 2, Incendiary: 1},
			UnusedUtilityValue: 900,
		},
	}
	statsTwo := &DemoPlayer{
		SteamID: 1, Name: "player-1-alt", UserID: 8, TeamID: 1,
		Deaths:       10,
		DeathsTraded: SideCount{Total: 2, CT: 1, T: 1},
		KillStats: KillStats{
			Total: 15, HeadShots: 6, WeaponsKills: map[string]int{"AK-47": 10, "Desert Eagle": 5},
			TradeKills: 2, TeamKills: 0,
		},
		AssistStats: AssistStats{Total: 7, FlashedEnemies: 1, DamageGiven: 1500},
		PlayerMapStats: PlayerMapStats{
			MVPs: 2, ACEs: 0, MultiKills: MultiKillRounds{K2: 2, K4: 1}, ClutchesWon: 1,
		},
		OpeningDuelStats: OpeningDuelStats{
			OpeningKills:  SideCount{Total: 3, CT: 1, T: 2},
			OpeningDeaths: SideCount{Total: 4, CT: 2, T: 2},
		},
		SideStats: SideStats{
			Rounds: SideCount{Total: 23, CT: 11, T: 12},
			Kills:  SideCount{Total: 15, CT: 7, T: 8},
			Deaths: SideCount{Total: 10, CT: 6, T: 4},
		},
		UtilityStats: UtilityStats{
			EnemiesFlashed: 4, FriendsFlashed: 1, EnemyFlashTimeSeconds: 5.25,
			UtilityDamage:      UtilityDamageStats{Total: 40, HE: 10, Fire: 30},
			GrenadesThrown:     GrenadesThrownStats{Total: 11, Flash: 4, Smoke: 3, HE: 2, Molotov: 1, Incendiary: 0, Decoy: 1},
			UnusedUtilityValue: 400,
		},
	}
	m1.Players[1] = statsOne
	m2.Players[1] = statsTwo

	series, err := BuildSeries(3, seriesInputs(m1, m2))
	if err != nil {
		t.Fatalf("BuildSeries: %v", err)
	}
	player := series.Players[1]
	if player.MapsPlayed != 2 || player.Rounds != 45 {
		t.Errorf("maps/rounds = %d/%d, want 2/45", player.MapsPlayed, player.Rounds)
	}
	if player.Deaths != 22 {
		t.Errorf("deaths = %d, want 22", player.Deaths)
	}
	if player.KillStats.Total != 35 || player.KillStats.HeadShots != 15 ||
		player.KillStats.TradeKills != 6 || player.KillStats.TeamKills != 1 {
		t.Errorf("kill stats = %+v, want exact sums", player.KillStats)
	}
	wantWeapons := map[string]int{"AK-47": 22, "AWP": 8, "Desert Eagle": 5}
	if !reflect.DeepEqual(player.KillStats.WeaponsKills, wantWeapons) {
		t.Errorf("weapon kills = %v, want %v", player.KillStats.WeaponsKills, wantWeapons)
	}
	if player.AssistStats.Total != 12 || player.AssistStats.FlashedEnemies != 3 || player.AssistStats.DamageGiven != 3300 {
		t.Errorf("assist stats = %+v, want exact sums", player.AssistStats)
	}
	if player.PlayerStats.MVPs != 5 || player.PlayerStats.ACEs != 1 || player.PlayerStats.ClutchesWon != 3 {
		t.Errorf("map stats = %+v, want exact sums", player.PlayerStats)
	}
	if want := (MultiKillRounds{K2: 5, K3: 1, K4: 1, K5: 1}); player.PlayerStats.MultiKills != want {
		t.Errorf("multi-kills = %+v, want %+v", player.PlayerStats.MultiKills, want)
	}
	if want := (SideCount{Total: 5, CT: 3, T: 2}); player.DeathsTraded != want {
		t.Errorf("deaths traded = %+v, want %+v", player.DeathsTraded, want)
	}
	if want := (SideCount{Total: 7, CT: 4, T: 3}); player.OpeningDuelStats.OpeningKills != want {
		t.Errorf("opening kills = %+v, want %+v", player.OpeningDuelStats.OpeningKills, want)
	}
	if want := (SideCount{Total: 45, CT: 23, T: 22}); player.SideStats.Rounds != want {
		t.Errorf("side rounds = %+v, want %+v", player.SideStats.Rounds, want)
	}
	utility := player.UtilityStats
	if utility.EnemiesFlashed != 10 || utility.FriendsFlashed != 3 || utility.EnemyFlashTimeSeconds != 15.75 ||
		utility.UnusedUtilityValue != 1300 {
		t.Errorf("utility stats = %+v, want exact sums", utility)
	}
	if utility.UtilityDamage != (UtilityDamageStats{Total: 130, HE: 70, Fire: 60}) {
		t.Errorf("utility damage = %+v, want exact sums", utility.UtilityDamage)
	}
	if utility.GrenadesThrown != (GrenadesThrownStats{Total: 26, Flash: 9, Smoke: 7, HE: 5, Molotov: 3, Incendiary: 1, Decoy: 1}) {
		t.Errorf("grenades thrown = %+v, want exact sums", utility.GrenadesThrown)
	}
	if utility.AverageEnemyFlashTimeSeconds != 1.575 {
		t.Errorf("average enemy flash time = %v, want 15.75/10", utility.AverageEnemyFlashTimeSeconds)
	}
	if got, want := player.KillStats.Precision, 15.0/35.0; got != want {
		t.Errorf("precision = %v, want %v", got, want)
	}
	if got, want := player.AssistStats.ADR, 3300.0/45.0; got != want {
		t.Errorf("ADR = %v, want damage over the 45 series rounds", got)
	}
	if !slices.Equal(player.Aliases, []string{"player-1", "player-1-alt"}) {
		t.Errorf("aliases = %v, want map-order dedup", player.Aliases)
	}
	if player.Name != "player-1" {
		t.Errorf("name = %q, want the first-observed of the tied names", player.Name)
	}
}

// TestBuildSeriesRecomputesRatesFromExactFacts drives the fact-based rates:
// KAST and its side splits, side ADR, opening success, the approximate
// percentages and the recomputed rating must all come from summed raw
// accumulators over the series denominator.
func TestBuildSeriesRecomputesRatesFromExactFacts(t *testing.T) {
	m1 := abMap("de_one", 13, 9)  // 22 rounds
	m2 := abMap("de_two", 13, 10) // 23 rounds
	m1.Players[1].SideStats.Rounds = SideCount{Total: 22, CT: 12, T: 10}
	m2.Players[1].SideStats.Rounds = SideCount{Total: 23, CT: 11, T: 12}
	m1.Players[1].OpeningDuelStats.OpeningKills = SideCount{Total: 4, CT: 2, T: 2}
	m2.Players[1].OpeningDuelStats.OpeningKills = SideCount{Total: 2, CT: 1, T: 1}
	m1.Players[1].PlayerMapStats.MultiKills = MultiKillRounds{K2: 2}
	m2.Players[1].PlayerMapStats.MultiKills = MultiKillRounds{K3: 1}
	m1.aggFacts = map[uint64]playerAggFacts{1: {
		kastRounds:  SideCount{Total: 16, CT: 9, T: 7},
		sideDamage:  SideCount{Total: 1800, CT: 1000, T: 800},
		openingWins: 3,
		ecoKills:    14.3,
		ecoDamage:   1650.5,
		ecoSurvival: 8.2,
		ecoKast:     17.1,
		roundSwing:  0.62,
	}}
	m2.aggFacts = map[uint64]playerAggFacts{1: {
		kastRounds:  SideCount{Total: 15, CT: 8, T: 7},
		sideDamage:  SideCount{Total: 1500, CT: 700, T: 800},
		openingWins: 1,
		ecoKills:    10.1,
		ecoDamage:   1400.25,
		ecoSurvival: 7.4,
		ecoKast:     15.9,
		roundSwing:  -0.12,
	}}

	series, err := BuildSeries(3, seriesInputs(m1, m2))
	if err != nil {
		t.Fatalf("BuildSeries: %v", err)
	}
	player := series.Players[1]
	if got, want := player.PlayerStats.KAST, 100*perRound(31, 45); got != want {
		t.Errorf("KAST = %v, want 100*31/45", got)
	}
	if got, want := player.SideStats.KAST, (SideRate{CT: 100 * perRound(17, 23), T: 100 * perRound(14, 22)}); got != want {
		t.Errorf("side KAST = %+v, want %+v", got, want)
	}
	if got, want := player.SideStats.ADR, (SideRate{CT: perRound(1700, 23), T: perRound(1600, 22)}); got != want {
		t.Errorf("side ADR = %+v, want %+v", got, want)
	}
	if got, want := player.OpeningDuelStats.OpeningSuccessRate, 100*4.0/6.0; got != want {
		t.Errorf("opening success = %v, want 100*4/6", got)
	}
	// The expected fact sums repeat the builder's own runtime arithmetic —
	// typed float64 sums divided by the 45 series rounds — so every
	// comparison is exact rather than tolerance-based. Untyped constant
	// expressions would be folded exactly at compile time and disagree in
	// the last bit.
	summed := m1.aggFacts[1]
	summed.add(m2.aggFacts[1])
	rounds := float64(45)
	if got, want := player.PlayerStats.ApproxEKASTPercent, 100*(summed.ecoKast/rounds); got != want {
		t.Errorf("approx eKAST = %v, want 100*33/45", got)
	}
	if got, want := player.PlayerStats.ApproxRoundSwingPercent, 100*(summed.roundSwing/rounds); got != want {
		t.Errorf("approx swing = %v, want 100*0.5/45", got)
	}
	if player.Rating == nil {
		t.Fatal("rating omitted despite both maps carrying raw facts")
	}
	want := blendRating(ratingRound{
		killPoints: summed.ecoKills / rounds,
		ecoDamage:  summed.ecoDamage / rounds,
		survival:   summed.ecoSurvival / rounds,
		kast:       summed.ecoKast / rounds,
		multiKill:  multiKillPoints(MultiKillRounds{K2: 2, K3: 1}) / rounds,
		swing:      summed.roundSwing / rounds,
	})
	if *player.Rating != want {
		t.Errorf("rating = %+v, want the model run once over summed facts %+v", *player.Rating, want)
	}
}

// TestBuildSeriesOmitsRatingWithoutRawFacts pins the documented fallback: a
// map that was not produced by Analyse carries no raw rating facts, so
// the series rating is explicitly null instead of being reconstructed from
// rounded per-map ratings.
func TestBuildSeriesOmitsRatingWithoutRawFacts(t *testing.T) {
	series, err := BuildSeries(3, seriesInputs(abMap("de_one", 13, 9), abMap("de_two", 13, 10)))
	if err != nil {
		t.Fatalf("BuildSeries: %v", err)
	}
	for id, player := range series.Players {
		if player.Rating != nil {
			t.Errorf("player %d rating = %+v, want nil without raw facts", id, player.Rating)
		}
	}
}

func TestBuildSeriesDoesNotMutateInputs(t *testing.T) {
	m1 := abMap("de_one", 13, 9)
	m2 := abMap("de_two", 9, 13)
	m3 := abMap("de_three", 10, 13)
	m1.aggFacts = map[uint64]playerAggFacts{1: {ecoKills: 1.5, kastRounds: SideCount{Total: 3, CT: 2, T: 1}}}
	inputs := seriesInputs(m1, m2, m3)
	snapshots := []MapAnalysis{deepCopyDemo(t, m1), deepCopyDemo(t, m2), deepCopyDemo(t, m3)}

	if _, err := BuildSeries(3, inputs); err != nil {
		t.Fatalf("BuildSeries: %v", err)
	}
	for i, input := range inputs {
		if !reflect.DeepEqual(*input.Demo, snapshots[i]) {
			t.Errorf("map %d was mutated by aggregation", i+1)
		}
	}
}

// deepCopyDemo clones a MapAnalysis including its unexported facts, which
// json round-tripping would drop.
func deepCopyDemo(t *testing.T, demo *MapAnalysis) MapAnalysis {
	t.Helper()
	data, err := json.Marshal(demo)
	if err != nil {
		t.Fatalf("marshalling demo: %v", err)
	}
	var clone MapAnalysis
	if err := json.Unmarshal(data, &clone); err != nil {
		t.Fatalf("unmarshalling demo: %v", err)
	}
	clone.aggFacts = maps.Clone(demo.aggFacts)
	return clone
}

func TestBuildSeriesIsDeterministic(t *testing.T) {
	build := func() *SeriesAnalysis {
		m1 := abMap("de_one", 13, 9)
		m2 := testMapDemo("de_two",
			testTeam{id: 1, name: "Bravo", roster: rosterB, score: 9},
			testTeam{id: 2, name: "Alpha", roster: rosterA, score: 13})
		series, err := BuildSeries(3, seriesInputs(m1, m2))
		if err != nil {
			t.Fatalf("BuildSeries: %v", err)
		}
		return series
	}
	if first, second := build(), build(); !reflect.DeepEqual(first, second) {
		t.Error("BuildSeries output differs between identical runs")
	}
}

// TestBuildSeriesKeepsUnrosteredPlayers pins how a player that appears in a
// map's raw events without ever playing an accepted round aggregates: no
// team, no maps played, and a zero denominator rather than an invented one.
func TestBuildSeriesKeepsUnrosteredPlayers(t *testing.T) {
	m1 := abMap("de_one", 13, 9)
	m1.Players[99] = &DemoPlayer{SteamID: 99, Name: "spectating-sub", KillStats: KillStats{WeaponsKills: map[string]int{}}}
	series, err := BuildSeries(3, seriesInputs(m1, abMap("de_two", 13, 10)))
	if err != nil {
		t.Fatalf("BuildSeries: %v", err)
	}
	bystander := series.Players[99]
	if bystander == nil {
		t.Fatal("unrostered player missing from aggregate players")
	}
	if bystander.TeamID != 0 || bystander.MapsPlayed != 0 || bystander.Rounds != 0 {
		t.Errorf("unrostered player = team %d, maps %d, rounds %d; want 0/0/0",
			bystander.TeamID, bystander.MapsPlayed, bystander.Rounds)
	}
}

func TestSelectSeriesPlayersResolvesAliasesAcrossMaps(t *testing.T) {
	m1 := abMap("de_one", 13, 9)
	m2 := abMap("de_two", 13, 10)
	m2.Players[1].Name = "renamed-one"
	series, err := BuildSeries(3, seriesInputs(m1, m2))
	if err != nil {
		t.Fatalf("BuildSeries: %v", err)
	}

	selected, err := SelectSeriesPlayers(series, []string{"RENAMED-ONE", "Player-6", "player-6"})
	if err != nil {
		t.Fatalf("SelectSeriesPlayers: %v", err)
	}
	want := map[uint64]bool{1: true, 6: true}
	if !reflect.DeepEqual(selected, want) {
		t.Errorf("selection = %v, want %v", selected, want)
	}

	_, err = SelectSeriesPlayers(series, []string{"nobody"})
	if err == nil || !strings.Contains(err.Error(), "available players") {
		t.Fatalf("SelectSeriesPlayers error = %v, want the available players listed", err)
	}
}

func TestSelectSeriesPlayersAmbiguousAliasFails(t *testing.T) {
	m1 := abMap("de_one", 13, 9)
	m2 := abMap("de_two", 13, 10)
	m1.Players[2].Name = "smurf"
	m2.Players[7].Name = "Smurf"
	series, err := BuildSeries(3, seriesInputs(m1, m2))
	if err != nil {
		t.Fatalf("BuildSeries: %v", err)
	}
	_, err = SelectSeriesPlayers(series, []string{"SMURF"})
	var ambiguity *PlayerAliasAmbiguityError
	if !errors.As(err, &ambiguity) {
		t.Fatalf("SelectSeriesPlayers error = %v, want a *PlayerAliasAmbiguityError", err)
	}
	if !slices.Equal(ambiguity.SteamIDs, []uint64{2, 7}) {
		t.Errorf("ambiguous candidates = %v, want [2 7]", ambiguity.SteamIDs)
	}
}

func TestFilterSeriesPlayersNarrowsWithoutTouchingTeams(t *testing.T) {
	series, err := BuildSeries(3, seriesInputs(abMap("de_one", 13, 9), abMap("de_two", 13, 10)))
	if err != nil {
		t.Fatalf("BuildSeries: %v", err)
	}
	filtered := FilterSeriesPlayers(series, map[uint64]bool{1: true})
	if len(filtered.Players) != 1 || filtered.Players[1] == nil {
		t.Errorf("filtered aggregate players = %d, want only SteamID 1", len(filtered.Players))
	}
	for i, seriesMap := range filtered.Maps {
		if len(seriesMap.Analysis.Players) != 1 || seriesMap.Analysis.Players[1] == nil {
			t.Errorf("filtered map %d players = %d, want only SteamID 1", i+1, len(seriesMap.Analysis.Players))
		}
		if !reflect.DeepEqual(seriesMap.Analysis.Teams, series.Maps[i].Analysis.Teams) {
			t.Errorf("filtered map %d teams changed", i+1)
		}
		if !reflect.DeepEqual(seriesMap.TeamAssignments, series.Maps[i].TeamAssignments) {
			t.Errorf("filtered map %d assignments changed", i+1)
		}
	}
	if !reflect.DeepEqual(filtered.Teams, series.Teams) {
		t.Error("filtering changed the series teams")
	}
	if len(series.Players) != 10 {
		t.Errorf("original series was narrowed to %d players; filtering must copy", len(series.Players))
	}
	for i, seriesMap := range series.Maps {
		if len(seriesMap.Analysis.Players) != 10 {
			t.Errorf("original map %d was narrowed to %d players; filtering must copy", i+1, len(seriesMap.Analysis.Players))
		}
	}
}
