// This file is the exported window onto the exact aggregation facts (issue
// #7): the raw accumulators behind every derived rate and the rating, so
// callers can snapshot them — the CLI's local history stores them per player —
// and later recompute cross-map trends from exact numerators instead of
// combining rounded per-map output. It is pure data access: no I/O, and the
// MapAnalysis JSON envelope is untouched.

package analysis

// PlayerAggregationFacts is one player's exact aggregation facts: the raw
// numerators a map's derived percentages and rating were computed from. The
// fields mirror the unexported per-map accumulators, so summing these values
// across maps and re-running the shared formulas reproduces series-style
// aggregation exactly. The JSON tags exist for callers that persist
// snapshots; MapAnalysis itself still marshals without any facts.
type PlayerAggregationFacts struct {
	// KASTRounds counts the rounds with classic KAST credit, total and per
	// side.
	KASTRounds SideCount `json:"kast_rounds"`
	// SideDamage is the damage given, total and per side.
	SideDamage SideCount `json:"side_damage"`
	// OpeningRoundsWon counts the rounds won after taking the opening kill.
	OpeningRoundsWon int `json:"opening_rounds_won"`
	// EcoKills is the eco-adjusted kill points.
	EcoKills float64 `json:"eco_kills"`
	// EcoDamage is the eco-adjusted damage given.
	EcoDamage float64 `json:"eco_damage"`
	// EcoSurvival is the eco-weighted rounds survived.
	EcoSurvival float64 `json:"eco_survival"`
	// EcoRatingKAST is the eco-weighted rounds with rating KAST credit.
	EcoRatingKAST float64 `json:"eco_rating_kast"`
	// RoundSwing is the summed, signed round-win-probability swing.
	RoundSwing float64 `json:"round_swing"`
}

// PlayerAggregationFacts snapshots the exact aggregation facts of the
// players this MapAnalysis currently exposes, keyed by SteamID64 with
// exactly the keys of Players — a view narrowed by FilterSeriesPlayers
// reports only its visible players, never the players filtered out of it.
// The returned map and its values are defensive copies sharing nothing with
// the analysis, so mutating them cannot corrupt series aggregation. It
// returns nil when the facts are unavailable rather than zero: for a
// MapAnalysis not produced by Analyse — a hand-built or JSON-decoded value —
// and for one whose players the facts do not fully cover.
func (m *MapAnalysis) PlayerAggregationFacts() map[uint64]PlayerAggregationFacts {
	if m.aggFacts == nil {
		return nil
	}
	facts := make(map[uint64]PlayerAggregationFacts, len(m.Players))
	for id := range m.Players {
		f, ok := m.aggFacts[id]
		if !ok {
			return nil
		}
		facts[id] = PlayerAggregationFacts{
			KASTRounds:       f.kastRounds,
			SideDamage:       f.sideDamage,
			OpeningRoundsWon: f.openingWins,
			EcoKills:         f.ecoKills,
			EcoDamage:        f.ecoDamage,
			EcoSurvival:      f.ecoSurvival,
			EcoRatingKAST:    f.ecoKast,
			RoundSwing:       f.roundSwing,
		}
	}
	return facts
}
