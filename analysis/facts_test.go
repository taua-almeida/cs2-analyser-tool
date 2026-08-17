package analysis

import (
	"encoding/json"
	"testing"
)

// factsFixture builds a MapAnalysis carrying two players' known raw
// accumulators, the way Analyse's exportAggFacts fills them in.
func factsFixture() *MapAnalysis {
	return &MapAnalysis{
		Players: map[uint64]*DemoPlayer{
			1: {SteamID: 1, Name: "one"},
			2: {SteamID: 2, Name: "two"},
		},
		aggFacts: map[uint64]playerAggFacts{
			1: {
				kastRounds:  SideCount{Total: 17, CT: 9, T: 8},
				sideDamage:  SideCount{Total: 2101, CT: 1200, T: 901},
				openingWins: 4,
				ecoKills:    18.25,
				ecoDamage:   1990.5,
				ecoSurvival: 8.75,
				ecoKast:     16.5,
				roundSwing:  -1.375,
			},
			2: {kastRounds: SideCount{Total: 12, CT: 12}},
		},
	}
}

// TestPlayerAggregationFactsExposesExactValues pins the field-by-field
// translation from the internal accumulators to the exported snapshot: every
// value crosses unchanged, and the snapshot covers exactly the players.
func TestPlayerAggregationFactsExposesExactValues(t *testing.T) {
	facts := factsFixture().PlayerAggregationFacts()
	if len(facts) != 2 {
		t.Fatalf("facts for %d players, want 2", len(facts))
	}
	want := PlayerAggregationFacts{
		KASTRounds:       SideCount{Total: 17, CT: 9, T: 8},
		SideDamage:       SideCount{Total: 2101, CT: 1200, T: 901},
		OpeningRoundsWon: 4,
		EcoKills:         18.25,
		EcoDamage:        1990.5,
		EcoSurvival:      8.75,
		EcoRatingKAST:    16.5,
		RoundSwing:       -1.375,
	}
	if facts[1] != want {
		t.Errorf("facts[1] = %+v, want %+v", facts[1], want)
	}
	if got := facts[2].KASTRounds; got != (SideCount{Total: 12, CT: 12}) {
		t.Errorf("facts[2].KASTRounds = %+v, want the stored counter", got)
	}
}

// TestPlayerAggregationFactsIsDefensive mutates one snapshot and requires a
// second call to still see the original values: the exported map shares
// nothing with the analysis.
func TestPlayerAggregationFactsIsDefensive(t *testing.T) {
	demo := factsFixture()
	first := demo.PlayerAggregationFacts()
	first[1] = PlayerAggregationFacts{EcoKills: -999}
	delete(first, 2)

	second := demo.PlayerAggregationFacts()
	if second[1].EcoKills != 18.25 {
		t.Errorf("facts[1].EcoKills = %v after mutating an earlier snapshot, want 18.25", second[1].EcoKills)
	}
	if _, ok := second[2]; !ok {
		t.Error("facts[2] missing after deleting it from an earlier snapshot")
	}
}

// TestPlayerAggregationFactsNilWithoutAnalyse pins that a MapAnalysis not
// produced by Analyse reports its facts as unavailable, the same nil
// BuildSeries treats as facts-missing, never as an empty-but-known map.
func TestPlayerAggregationFactsNilWithoutAnalyse(t *testing.T) {
	demo := &MapAnalysis{Players: map[uint64]*DemoPlayer{1: {SteamID: 1}}}
	if facts := demo.PlayerAggregationFacts(); facts != nil {
		t.Errorf("facts = %v for a hand-built MapAnalysis, want nil", facts)
	}
}

// TestPlayerAggregationFactsFollowsFilteredView pins the snapshot to the
// players the view exposes: FilterSeriesPlayers narrows each embedded map's
// Players while its shallow copy keeps the full internal facts, and the
// snapshot must report exactly the visible players — never the filtered-out
// ones.
func TestPlayerAggregationFactsFollowsFilteredView(t *testing.T) {
	series := &SeriesAnalysis{
		Players: map[uint64]*SeriesPlayer{1: {SteamID: 1}, 2: {SteamID: 2}},
		Maps:    []SeriesMap{{SHA256: "aaa", Analysis: factsFixture()}},
	}
	filtered := FilterSeriesPlayers(series, map[uint64]bool{1: true})

	facts := filtered.Maps[0].Analysis.PlayerAggregationFacts()
	if len(facts) != 1 {
		t.Fatalf("filtered view exposes facts for %d players, want only the visible 1", len(facts))
	}
	if _, ok := facts[1]; !ok {
		t.Error("visible player 1 has no facts")
	}
	if _, ok := facts[2]; ok {
		t.Error("filtered-out player 2 leaked into the facts snapshot")
	}
	// The original series is untouched and still reports everyone.
	if full := series.Maps[0].Analysis.PlayerAggregationFacts(); len(full) != 2 {
		t.Errorf("original analysis facts = %d players after filtering a copy, want 2", len(full))
	}
}

// TestPlayerAggregationFactsNilWhenPlayersUncovered pins the unavailable
// verdict for an inconsistent value whose players the facts do not cover:
// fabricating zero facts for a real player would poison additive trends.
func TestPlayerAggregationFactsNilWhenPlayersUncovered(t *testing.T) {
	demo := factsFixture()
	demo.Players[3] = &DemoPlayer{SteamID: 3, Name: "three"}
	if facts := demo.PlayerAggregationFacts(); facts != nil {
		t.Errorf("facts = %v with an uncovered player, want nil (unavailable)", facts)
	}
}

// TestMapAnalysisJSONOmitsFacts pins that carrying aggregation facts does not
// change the marshalled MapAnalysis: the envelope keeps exactly its four keys.
func TestMapAnalysisJSONOmitsFacts(t *testing.T) {
	data, err := json.Marshal(factsFixture())
	if err != nil {
		t.Fatalf("marshalling: %v", err)
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatalf("unmarshalling envelope: %v", err)
	}
	for _, key := range []string{"players", "teams", "map_data", "game_mode"} {
		if _, ok := envelope[key]; !ok {
			t.Errorf("envelope is missing key %q", key)
		}
	}
	if len(envelope) != 4 {
		t.Errorf("envelope has %d keys, want the 4 stable ones", len(envelope))
	}
}
