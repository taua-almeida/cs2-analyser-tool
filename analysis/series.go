// This file is the series aggregation (issue #34): an explicit, completed
// BO3 or BO5 built from maps that were each parsed with Analyse. Everything
// in it is pure — no flag handling, rendering or filesystem access. The
// series is never inferred: the caller states the format, and the maps
// arrive in supplied order.

package analysis

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
)

// SeriesMapInput is one played map of a series. Position in the BuildSeries
// slice is the map's supplied order — the CLI takes it from the repeated
// --demo flags — and it is preserved everywhere downstream.
type SeriesMapInput struct {
	// Demo is the parsed map. BuildSeries never mutates it.
	Demo *MapAnalysis
	// SHA256 is the lowercase hex digest of the demo file's bytes. It
	// identifies the map's content, so two inputs with equal digests are
	// rejected as the same demo regardless of their file paths.
	SHA256 string
}

// SeriesTeamAssignment records which series team one map-local team resolved
// to. It is the explicit bridge between the two ID scopes: map_team_id is
// only meaningful inside its own map, series_team_id only inside this series.
type SeriesTeamAssignment struct {
	MapTeamID    int `json:"map_team_id"`
	SeriesTeamID int `json:"series_team_id"`
}

// SeriesMap is one map of the series, in supplied order: its content digest,
// its winner in series-team terms, the local-to-series team translation, and
// the unchanged standalone analysis.
type SeriesMap struct {
	SHA256          string                 `json:"sha256"`
	WinnerTeamID    int                    `json:"winner_team_id"`
	TeamAssignments []SeriesTeamAssignment `json:"team_assignments"`
	Analysis        *MapAnalysis           `json:"analysis"`
}

// SeriesTeam is one of the two series-scoped teams. TeamID is series-local —
// team 1 is the first map's first logical team — and claims no identity
// beyond this one series. Name and Aliases follow the map-level label rules:
// aliases are the deduplicated map-level aliases in first map-observation
// order, and the display name is the most-observed nonempty map-level name,
// ties broken by first observation.
type SeriesTeam struct {
	TeamID    int      `json:"team_id"`
	Name      string   `json:"name"`
	Aliases   []string `json:"aliases"`
	MapsWon   int      `json:"maps_won"`
	RoundsWon int      `json:"rounds_won"`
	Roster    []uint64 `json:"roster"`
}

// SeriesPlayer is one player aggregated across the maps they appear in,
// matched exclusively by nonzero SteamID64. It deliberately has no user_id:
// server user IDs are map-local and meaningless across demos. Additive
// fields are exact sums of the per-map values; every rate inside the reused
// stat structs is recomputed from exact summed numerators over the player's
// series round count, never from the per-map rates.
type SeriesPlayer struct {
	SteamID uint64 `json:"steam_id"`
	// Name is the most-observed nonempty per-map display name, ties broken
	// by first observation — the same rule series team names use.
	Name string `json:"name"`
	// Aliases are the deduplicated nonempty per-map display names in map
	// order.
	Aliases []string `json:"aliases"`
	// TeamID is the series team whose roster contains this SteamID, or 0
	// for a player who never played an accepted round for a logical team.
	TeamID int `json:"team_id"`
	// MapsPlayed counts the maps where this SteamID participated for a
	// logical team. Rounds sums those maps' total rounds and is the
	// denominator of every match-wide rate, mirroring how a single map
	// divides by the whole map's rounds.
	MapsPlayed       int              `json:"maps_played"`
	Rounds           int              `json:"rounds"`
	Deaths           int              `json:"deaths"`
	DeathsTraded     SideCount        `json:"deaths_traded"`
	KillStats        KillStats        `json:"kill_stats"`
	AssistStats      AssistStats      `json:"assist_stats"`
	PlayerStats      PlayerMapStats   `json:"player_series_stats"`
	OpeningDuelStats OpeningDuelStats `json:"opening_duel_stats"`
	SideStats        SideStats        `json:"side_stats"`
	UtilityStats     UtilityStats     `json:"utility_stats"`
	// Rating is recomputed by running the rating model once over the exact
	// summed raw facts (eco-adjusted kills and damage, eco-weighted survival
	// and rating-KAST, signed swing, multi-kill buckets) with the series
	// round denominator. It is null — never approximated from map ratings —
	// when a map was not produced by Analyse and so carries no raw
	// facts, or when the player has no series rounds.
	Rating *RatingStats `json:"rating"`
}

// SeriesAnalysis is a completed BO3/BO5: the winner, the two series-scoped
// teams, the aggregate players keyed by SteamID, and the ordered per-map
// results with their unchanged standalone analyses.
type SeriesAnalysis struct {
	BestOf       int                      `json:"best_of"`
	WinnerTeamID int                      `json:"winner_team_id"`
	Teams        []SeriesTeam             `json:"teams"`
	Players      map[uint64]*SeriesPlayer `json:"players"`
	Maps         []SeriesMap              `json:"maps"`
}

// SeriesMapTeams records one map's two local teams for identity errors: the
// 0-based supplied map index, the map-local team IDs in the map's own team
// order, and their rosters.
type SeriesMapTeams struct {
	MapIndex int
	TeamIDs  [2]int
	Rosters  [2][]uint64
}

// SeriesAssignmentCandidate is one enumerated way of assigning every map's
// two local teams onto the two series teams.
type SeriesAssignmentCandidate struct {
	// Assignments[i] holds map i's two local-to-series pairs, in the map's
	// team order.
	Assignments [][2]SeriesTeamAssignment
	// Conflicts lists the SteamIDs this candidate would place on both
	// series teams, sorted ascending; it is empty for a valid candidate.
	Conflicts []uint64
}

func (c SeriesAssignmentCandidate) describe() string {
	parts := make([]string, len(c.Assignments))
	for i, pair := range c.Assignments {
		parts[i] = fmt.Sprintf("map %d: %d→%d, %d→%d", i+1,
			pair[0].MapTeamID, pair[0].SeriesTeamID, pair[1].MapTeamID, pair[1].SeriesTeamID)
	}
	return strings.Join(parts, "; ")
}

func describeSeriesMapTeams(maps []SeriesMapTeams) string {
	parts := make([]string, len(maps))
	for i, m := range maps {
		parts[i] = fmt.Sprintf("map %d: team %d %v, team %d %v",
			m.MapIndex+1, m.TeamIDs[0], m.Rosters[0], m.TeamIDs[1], m.Rosters[1])
	}
	return strings.Join(parts, "; ")
}

// SeriesTeamConflictError reports that no joint roster assignment keeps
// every SteamID on one series team: the supplied maps mix players across
// lineups rather than replaying the same two teams.
type SeriesTeamConflictError struct {
	Maps []SeriesMapTeams
	// Candidates holds every enumerated assignment with the SteamIDs it
	// would place on both teams.
	Candidates []SeriesAssignmentCandidate
}

func (e *SeriesTeamConflictError) Error() string {
	var b strings.Builder
	b.WriteString("cannot resolve series teams: no joint roster assignment keeps every SteamID on one team")
	for _, candidate := range e.Candidates {
		fmt.Fprintf(&b, "; [%s] places %v on both teams", candidate.describe(), candidate.Conflicts)
	}
	fmt.Fprintf(&b, "; rosters: %s", describeSeriesMapTeams(e.Maps))
	return b.String()
}

// SeriesTeamAmbiguityError reports that more than one joint roster
// assignment is consistent with the supplied maps, so the series teams
// cannot be resolved without guessing. Clan names never break the tie: they
// are labels, not identity.
type SeriesTeamAmbiguityError struct {
	Maps []SeriesMapTeams
	// Candidates holds the competing valid assignments.
	Candidates []SeriesAssignmentCandidate
}

func (e *SeriesTeamAmbiguityError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "series team assignment is ambiguous: %d joint roster assignments are consistent", len(e.Candidates))
	for _, candidate := range e.Candidates {
		fmt.Fprintf(&b, "; [%s]", candidate.describe())
	}
	fmt.Fprintf(&b, "; rosters: %s", describeSeriesMapTeams(e.Maps))
	return b.String()
}

// PlayerAliasAmbiguityError reports a requested player alias that matches
// more than one SteamID across the series.
type PlayerAliasAmbiguityError struct {
	Alias    string
	SteamIDs []uint64 // sorted candidates
}

func (e *PlayerAliasAmbiguityError) Error() string {
	return fmt.Sprintf("player %q matches more than one SteamID in this series: %v; request a unique alias",
		e.Alias, e.SteamIDs)
}

// BuildSeries aggregates a completed best-of-3 or best-of-5 from maps in
// supplied order. It validates each map's logical-team shape, rejects
// duplicate demo content by digest, resolves the two series teams through a
// unique joint roster assignment, and requires the series to be clinched
// exactly on the final supplied map. The inputs are never mutated.
func BuildSeries(bestOf int, maps []SeriesMapInput) (*SeriesAnalysis, error) {
	// The minimum played-map count doubles as the clinch threshold: a series
	// is complete exactly when one team has won that many maps.
	threshold, maxMaps, err := SeriesMapCountRange(bestOf)
	if err != nil {
		return nil, err
	}
	if len(maps) < threshold || len(maps) > maxMaps {
		return nil, fmt.Errorf("a completed best-of-%d has %s maps, got %d",
			bestOf, seriesMapCountChoices(bestOf), len(maps))
	}
	if err := validateSeriesMaps(maps); err != nil {
		return nil, err
	}

	orientation, err := resolveSeriesTeams(maps)
	if err != nil {
		return nil, err
	}
	tally, err := seriesResults(maps, orientation, bestOf, threshold)
	if err != nil {
		return nil, err
	}

	teams := buildSeriesTeams(maps, orientation, tally)
	return &SeriesAnalysis{
		BestOf:       bestOf,
		WinnerTeamID: tally.winner,
		Teams:        teams,
		Players:      buildSeriesPlayers(maps, teams),
		Maps:         buildSeriesMaps(maps, orientation, tally.mapWinners),
	}, nil
}

// SeriesMapCountRange validates a series format and returns the played-map
// counts a completed series can have: a best-of-n is decided somewhere
// between its clinch threshold, n/2+1, and its full length. It is the one
// home of the format rule, shared with the CLI's fail-before-parse flag
// validation.
func SeriesMapCountRange(bestOf int) (minMaps, maxMaps int, err error) {
	if bestOf != 3 && bestOf != 5 {
		return 0, 0, fmt.Errorf("invalid best-of %d, must be 3 or 5", bestOf)
	}
	return bestOf/2 + 1, bestOf, nil
}

// seriesMapCountChoices spells out the accepted played-map counts of a
// validated format.
func seriesMapCountChoices(bestOf int) string {
	if bestOf == 5 {
		return "3, 4 or 5"
	}
	return "2 or 3"
}

// seriesMapLabel names a map in errors by its 1-based supplied position and,
// when known, its map name.
func seriesMapLabel(index int, demo *MapAnalysis) string {
	if demo != nil && demo.Map.MapName != "" {
		return fmt.Sprintf("map %d (%s)", index+1, demo.Map.MapName)
	}
	return fmt.Sprintf("map %d", index+1)
}

func validateSeriesMaps(inputs []SeriesMapInput) error {
	digests := make(map[string]int, len(inputs))
	for i, input := range inputs {
		if input.Demo == nil {
			return fmt.Errorf("map %d has no parsed demo", i+1)
		}
		if input.SHA256 == "" {
			return fmt.Errorf("%s has no SHA-256 digest", seriesMapLabel(i, input.Demo))
		}
		if first, duplicate := digests[input.SHA256]; duplicate {
			return fmt.Errorf("maps %d and %d are the same demo: both have SHA-256 %s",
				first+1, i+1, input.SHA256)
		}
		digests[input.SHA256] = i
		if err := validateSeriesMap(i, input.Demo); err != nil {
			return err
		}
	}
	return nil
}

// seriesTeamSize is the lineup size of the competitive 5v5 format, the only
// format series aggregation supports. A map whose logical team fielded fewer
// eligible players — a completed Wingman 2v2 would otherwise pass every
// other check — is rejected before identity resolution. Substitutions only
// ever make a roster larger, so the bound never rejects a competitive map
// with stand-ins.
const seriesTeamSize = 5

// seriesGameModes are the reported game modes accepted for a series map:
// the spellings of the classic competitive 5v5 ruleset, plus "" because a
// demo that names no mode and matches no known ruleset is unknown rather
// than known-unsupported. Any other mode — casual, deathmatch, wingman's
// scrimcomp2v2 — is rejected outright: the rating's win-probability model
// and denominators assume 5v5 rounds, so aggregating a larger or respawn
// format would produce numbers that look valid and mean nothing.
var seriesGameModes = map[string]bool{
	"":             true,
	"competitive":  true,
	"scrimcomp5v5": true,
	"premier":      true,
}

// validateSeriesMap checks one map holds a decided, internally consistent
// two-team competitive 5v5 match: a supported game mode, exactly two logical
// teams with distinct IDs and at least 5v5 rosters, untied scores that add
// up to the map's rounds, players that reference only those teams (or team
// 0, the no-team marker), and no more per-round participants than a 5v5
// lobby can produce.
func validateSeriesMap(index int, demo *MapAnalysis) error {
	label := seriesMapLabel(index, demo)
	if !seriesGameModes[demo.GameMode] {
		return fmt.Errorf("%s reports game mode %q; series aggregation supports only the competitive 5v5 format", label, demo.GameMode)
	}
	if len(demo.Teams) != 2 {
		return fmt.Errorf("%s has %d logical teams, want exactly 2", label, len(demo.Teams))
	}
	first, second := demo.Teams[0], demo.Teams[1]
	if first.TeamID == second.TeamID {
		return fmt.Errorf("%s repeats map-local team ID %d", label, first.TeamID)
	}
	for _, team := range demo.Teams {
		if len(team.Roster) < seriesTeamSize {
			return fmt.Errorf("%s team %d fielded %d players; series aggregation supports only the competitive 5v5 format, so a roster needs at least %d (substitutes only add more)",
				label, team.TeamID, len(team.Roster), seriesTeamSize)
		}
	}
	if first.Score == second.Score {
		return fmt.Errorf("%s ended tied %d:%d; a series map needs a winner", label, first.Score, second.Score)
	}
	if first.Score+second.Score != demo.Map.TotalRounds {
		return fmt.Errorf("%s team scores %d+%d do not add up to its %d rounds",
			label, first.Score, second.Score, demo.Map.TotalRounds)
	}
	for _, id := range slices.Sorted(maps.Keys(demo.Players)) {
		if teamID := demo.Players[id].TeamID; teamID != 0 && teamID != first.TeamID && teamID != second.TeamID {
			return fmt.Errorf("%s player %d references unknown map team ID %d", label, id, teamID)
		}
	}
	// The mode gate above cannot catch an oversized lobby whose demo names
	// no mode, so hold the per-round participation to what a 5v5 can
	// produce. A round involves the two lineups' ten players; the one
	// legitimate overage is a freeze-time swap counting both the leaver and
	// the joiner, so the ceiling allows one extra participant per round —
	// well below the ~20 of a 10v10 lobby.
	participantRounds := 0
	for _, player := range demo.Players {
		participantRounds += player.SideStats.Rounds.Total
	}
	if participantRounds > (2*seriesTeamSize+1)*demo.Map.TotalRounds {
		return fmt.Errorf("%s counts %d participant-rounds over %d rounds, more than a 5v5 lobby can produce; series aggregation supports only the competitive 5v5 format",
			label, participantRounds, demo.Map.TotalRounds)
	}
	return nil
}

// resolveSeriesTeams finds the unique joint assignment of every map's two
// local teams onto the two series teams. Series team 1 and 2 are seeded
// deterministically from the first map's team order; each later map either
// keeps that orientation or crosses it, and all combinations — at most 2^4
// for a BO5, so exhaustive evaluation is simpler and safer than a greedy
// matcher — are judged jointly against one rule: the union rosters of the
// two series teams must stay disjoint. Exactly one surviving combination is
// success, zero is a mixed-team conflict, several is an ambiguity; both
// failures carry the competing evidence. Map-local IDs, sides, filenames,
// map names, scores, player names and clan names are never identity.
func resolveSeriesTeams(inputs []SeriesMapInput) ([][2]int, error) {
	type orientationCandidate struct {
		orientation [][2]int
		conflicts   []uint64
	}
	candidates := make([]orientationCandidate, 0, 1<<(len(inputs)-1))
	var valid []orientationCandidate
	for mask := 0; mask < 1<<(len(inputs)-1); mask++ {
		orientation := make([][2]int, len(inputs))
		orientation[0] = [2]int{1, 2}
		for i := 1; i < len(inputs); i++ {
			if mask&(1<<(i-1)) == 0 {
				orientation[i] = [2]int{1, 2}
			} else {
				orientation[i] = [2]int{2, 1}
			}
		}
		candidate := orientationCandidate{
			orientation: orientation,
			conflicts:   assignmentConflicts(inputs, orientation),
		}
		candidates = append(candidates, candidate)
		if len(candidate.conflicts) == 0 {
			valid = append(valid, candidate)
		}
	}
	// The full evidence — every candidate's explicit local-to-series pairs —
	// is only assembled for the two failure verdicts.
	exportCandidates := func(list []orientationCandidate) []SeriesAssignmentCandidate {
		out := make([]SeriesAssignmentCandidate, len(list))
		for i, candidate := range list {
			out[i] = SeriesAssignmentCandidate{
				Assignments: assignmentPairs(inputs, candidate.orientation),
				Conflicts:   candidate.conflicts,
			}
		}
		return out
	}
	switch len(valid) {
	case 1:
		return valid[0].orientation, nil
	case 0:
		return nil, &SeriesTeamConflictError{Maps: seriesMapTeams(inputs), Candidates: exportCandidates(candidates)}
	default:
		return nil, &SeriesTeamAmbiguityError{Maps: seriesMapTeams(inputs), Candidates: exportCandidates(valid)}
	}
}

// assignmentConflicts returns the SteamIDs the orientation would place on
// both series teams, sorted ascending. Unioning every map — the first
// included — means a roster overlap inside a single map also surfaces here.
func assignmentConflicts(inputs []SeriesMapInput, orientation [][2]int) []uint64 {
	rosters := [3]map[uint64]bool{{}, {}, {}}
	for i, input := range inputs {
		for t, team := range input.Demo.Teams {
			for _, id := range team.Roster {
				rosters[orientation[i][t]][id] = true
			}
		}
	}
	var conflicts []uint64
	for id := range rosters[1] {
		if rosters[2][id] {
			conflicts = append(conflicts, id)
		}
	}
	slices.Sort(conflicts)
	return conflicts
}

func assignmentPairs(inputs []SeriesMapInput, orientation [][2]int) [][2]SeriesTeamAssignment {
	pairs := make([][2]SeriesTeamAssignment, len(inputs))
	for i, input := range inputs {
		for t, team := range input.Demo.Teams {
			pairs[i][t] = SeriesTeamAssignment{MapTeamID: team.TeamID, SeriesTeamID: orientation[i][t]}
		}
	}
	return pairs
}

func seriesMapTeams(inputs []SeriesMapInput) []SeriesMapTeams {
	out := make([]SeriesMapTeams, len(inputs))
	for i, input := range inputs {
		out[i] = SeriesMapTeams{
			MapIndex: i,
			TeamIDs:  [2]int{input.Demo.Teams[0].TeamID, input.Demo.Teams[1].TeamID},
			Rosters: [2][]uint64{
				slices.Clone(input.Demo.Teams[0].Roster),
				slices.Clone(input.Demo.Teams[1].Roster),
			},
		}
	}
	return out
}

// seriesTally is what walking the maps in order produces: per-series-team
// map and round wins (indexed by series team ID, slot 0 unused), each map's
// winner in series terms, and the series winner.
type seriesTally struct {
	mapWins    [3]int
	roundsWon  [3]int
	mapWinners []int
	winner     int
}

// seriesResults walks the maps in supplied order, translating each map's
// winning local team into series terms and enforcing the completed-series
// rule: the final supplied map must be the first point at which a team
// reaches the clinch threshold, so a map after the clinch and a series
// nobody clinched are both rejected.
func seriesResults(inputs []SeriesMapInput, orientation [][2]int, bestOf, threshold int) (seriesTally, error) {
	tally := seriesTally{mapWinners: make([]int, len(inputs))}
	for i, input := range inputs {
		teams := input.Demo.Teams
		winnerIndex := 0
		if teams[1].Score > teams[0].Score {
			winnerIndex = 1
		}
		for t, team := range teams {
			tally.roundsWon[orientation[i][t]] += team.Score
		}
		seriesWinner := orientation[i][winnerIndex]
		tally.mapWins[seriesWinner]++
		tally.mapWinners[i] = seriesWinner
		if tally.mapWins[seriesWinner] >= threshold {
			if i != len(inputs)-1 {
				return seriesTally{}, fmt.Errorf(
					"series team %d already clinched the best-of-%d %d:%d after %s; %s was supplied after the clinch",
					seriesWinner, bestOf, tally.mapWins[seriesWinner], tally.mapWins[3-seriesWinner],
					seriesMapLabel(i, input.Demo), seriesMapLabel(i+1, inputs[i+1].Demo))
			}
			tally.winner = seriesWinner
		}
	}
	if tally.winner == 0 {
		return seriesTally{}, fmt.Errorf(
			"series is not complete: no team reached %d map wins in the best-of-%d (map score %d:%d)",
			threshold, bestOf, tally.mapWins[1], tally.mapWins[2])
	}
	return tally, nil
}

// seriesTeamAcc accumulates one series team while the maps are walked once:
// the union roster, the map-level display-name observations, and the union
// of map-level aliases (a second tally reused purely for its ordered dedup).
type seriesTeamAcc struct {
	roster  map[uint64]bool
	names   nameTally
	aliases nameTally
}

func buildSeriesTeams(inputs []SeriesMapInput, orientation [][2]int, tally seriesTally) []SeriesTeam {
	acc := [2]seriesTeamAcc{{roster: map[uint64]bool{}}, {roster: map[uint64]bool{}}}
	for i, input := range inputs {
		for t, team := range input.Demo.Teams {
			teamAcc := &acc[orientation[i][t]-1]
			for _, id := range team.Roster {
				teamAcc.roster[id] = true
			}
			teamAcc.names.observe(team.Name)
			for _, alias := range team.Aliases {
				teamAcc.aliases.observe(alias)
			}
		}
	}
	teams := make([]SeriesTeam, len(acc))
	for i, teamAcc := range acc {
		teams[i] = SeriesTeam{
			TeamID:    i + 1,
			Name:      teamAcc.names.mostObserved(),
			Aliases:   teamAcc.aliases.names(),
			MapsWon:   tally.mapWins[i+1],
			RoundsWon: tally.roundsWon[i+1],
			Roster:    slices.Sorted(maps.Keys(teamAcc.roster)),
		}
	}
	return teams
}

func buildSeriesMaps(inputs []SeriesMapInput, orientation [][2]int, mapWinners []int) []SeriesMap {
	pairs := assignmentPairs(inputs, orientation)
	out := make([]SeriesMap, len(inputs))
	for i, input := range inputs {
		out[i] = SeriesMap{
			SHA256:          input.SHA256,
			WinnerTeamID:    mapWinners[i],
			TeamAssignments: pairs[i][:],
			Analysis:        input.Demo,
		}
	}
	return out
}

// seriesPlayerAgg accumulates one player across maps before the derived
// stats are computed.
type seriesPlayerAgg struct {
	player *SeriesPlayer
	names  nameTally
	facts  playerAggFacts
	// factsKnown stays true only while every map the player appears in
	// carries raw aggregation facts, i.e. was produced by Analyse.
	factsKnown bool
}

func buildSeriesPlayers(inputs []SeriesMapInput, teams []SeriesTeam) map[uint64]*SeriesPlayer {
	teamOf := make(map[uint64]int)
	for _, team := range teams {
		for _, id := range team.Roster {
			teamOf[id] = team.TeamID
		}
	}

	aggregates := make(map[uint64]*seriesPlayerAgg)
	for _, input := range inputs {
		demo := input.Demo
		participants := make(map[uint64]bool)
		for _, team := range demo.Teams {
			for _, id := range team.Roster {
				participants[id] = true
			}
		}
		for id, mapPlayer := range demo.Players {
			agg := aggregates[id]
			if agg == nil {
				agg = &seriesPlayerAgg{
					player: &SeriesPlayer{
						SteamID:   id,
						KillStats: KillStats{WeaponsKills: make(map[string]int)},
					},
					factsKnown: true,
				}
				aggregates[id] = agg
			}
			agg.names.observe(mapPlayer.Name)
			if participants[id] {
				agg.player.MapsPlayed++
				agg.player.Rounds += demo.Map.TotalRounds
			}
			agg.player.addMapStats(mapPlayer)
			if demo.aggFacts == nil {
				agg.factsKnown = false
			} else {
				agg.facts.add(demo.aggFacts[id])
			}
		}
	}

	players := make(map[uint64]*SeriesPlayer, len(aggregates))
	for id, agg := range aggregates {
		agg.player.Name = agg.names.mostObserved()
		agg.player.Aliases = agg.names.names()
		agg.player.TeamID = teamOf[id]
		agg.player.deriveSeriesStats(agg.facts, agg.factsKnown)
		players[id] = agg.player
	}
	return players
}

func (f *playerAggFacts) add(other playerAggFacts) {
	f.kastRounds.addCounts(other.kastRounds)
	f.sideDamage.addCounts(other.sideDamage)
	f.openingWins += other.openingWins
	f.ecoKills += other.ecoKills
	f.ecoDamage += other.ecoDamage
	f.ecoSurvival += other.ecoSurvival
	f.ecoKast += other.ecoKast
	f.roundSwing += other.roundSwing
}

// addMapStats folds one map's additive facts into the aggregate. Derived
// rates are left alone here: they are recomputed afterwards from exact
// summed numerators.
func (sp *SeriesPlayer) addMapStats(mp *DemoPlayer) {
	sp.Deaths += mp.Deaths
	sp.DeathsTraded.addCounts(mp.DeathsTraded)

	sp.KillStats.Total += mp.KillStats.Total
	sp.KillStats.HeadShots += mp.KillStats.HeadShots
	sp.KillStats.TradeKills += mp.KillStats.TradeKills
	sp.KillStats.TeamKills += mp.KillStats.TeamKills
	for weapon, kills := range mp.KillStats.WeaponsKills {
		sp.KillStats.WeaponsKills[weapon] += kills
	}

	sp.AssistStats.Total += mp.AssistStats.Total
	sp.AssistStats.FlashedEnemies += mp.AssistStats.FlashedEnemies
	sp.AssistStats.DamageGiven += mp.AssistStats.DamageGiven

	sp.PlayerStats.MVPs += mp.PlayerMapStats.MVPs
	sp.PlayerStats.ACEs += mp.PlayerMapStats.ACEs
	sp.PlayerStats.MultiKills.K2 += mp.PlayerMapStats.MultiKills.K2
	sp.PlayerStats.MultiKills.K3 += mp.PlayerMapStats.MultiKills.K3
	sp.PlayerStats.MultiKills.K4 += mp.PlayerMapStats.MultiKills.K4
	sp.PlayerStats.MultiKills.K5 += mp.PlayerMapStats.MultiKills.K5
	sp.PlayerStats.ClutchesWon += mp.PlayerMapStats.ClutchesWon

	sp.OpeningDuelStats.OpeningKills.addCounts(mp.OpeningDuelStats.OpeningKills)
	sp.OpeningDuelStats.OpeningDeaths.addCounts(mp.OpeningDuelStats.OpeningDeaths)

	sp.SideStats.Rounds.addCounts(mp.SideStats.Rounds)
	sp.SideStats.Kills.addCounts(mp.SideStats.Kills)
	sp.SideStats.Deaths.addCounts(mp.SideStats.Deaths)

	sp.UtilityStats.EnemiesFlashed += mp.UtilityStats.EnemiesFlashed
	sp.UtilityStats.FriendsFlashed += mp.UtilityStats.FriendsFlashed
	sp.UtilityStats.EnemyFlashTimeSeconds += mp.UtilityStats.EnemyFlashTimeSeconds
	sp.UtilityStats.UtilityDamage.Total += mp.UtilityStats.UtilityDamage.Total
	sp.UtilityStats.UtilityDamage.HE += mp.UtilityStats.UtilityDamage.HE
	sp.UtilityStats.UtilityDamage.Fire += mp.UtilityStats.UtilityDamage.Fire
	sp.UtilityStats.GrenadesThrown.Total += mp.UtilityStats.GrenadesThrown.Total
	sp.UtilityStats.GrenadesThrown.Flash += mp.UtilityStats.GrenadesThrown.Flash
	sp.UtilityStats.GrenadesThrown.Smoke += mp.UtilityStats.GrenadesThrown.Smoke
	sp.UtilityStats.GrenadesThrown.HE += mp.UtilityStats.GrenadesThrown.HE
	sp.UtilityStats.GrenadesThrown.Molotov += mp.UtilityStats.GrenadesThrown.Molotov
	sp.UtilityStats.GrenadesThrown.Incendiary += mp.UtilityStats.GrenadesThrown.Incendiary
	sp.UtilityStats.GrenadesThrown.Decoy += mp.UtilityStats.GrenadesThrown.Decoy
	sp.UtilityStats.UnusedUtilityValue += mp.UtilityStats.UnusedUtilityValue
}

func (sp *SeriesPlayer) statRefs() statRefs {
	return statRefs{
		kills:   &sp.KillStats,
		assists: &sp.AssistStats,
		stats:   &sp.PlayerStats,
		opening: &sp.OpeningDuelStats,
		side:    &sp.SideStats,
		utility: &sp.UtilityStats,
	}
}

// deriveSeriesStats runs the same deriveStats formulas a single map's derive
// uses, with the player's series round count as the match-wide denominator.
// When raw facts are unavailable (a map not produced by Analyse), the
// fact-based values read 0 and the rating stays nil rather than being
// reconstructed from rounded per-map output.
func (sp *SeriesPlayer) deriveSeriesStats(facts playerAggFacts, factsKnown bool) {
	rating, rated := deriveStats(sp.statRefs(), facts, sp.Rounds)
	if rated && factsKnown {
		sp.Rating = &rating
	}
}

// SelectSeriesPlayers resolves requested names against the aggregate
// players' collected aliases, case-insensitively, once cross-map identity
// and alias collection are done. Duplicate requests count once. Selection is
// all-or-nothing: a name matching no player fails listing every available
// player, and a name matching more than one SteamID fails with a
// PlayerAliasAmbiguityError carrying the sorted candidates. The returned set
// only narrows rendering and saving; it never feeds back into team
// resolution or aggregation.
func SelectSeriesPlayers(series *SeriesAnalysis, requestedNames []string) (map[uint64]bool, error) {
	selected := make(map[uint64]bool)
	var unmatched []string
	for _, name := range distinctNamesFold(requestedNames) {
		var matches []uint64
		for id, player := range series.Players {
			hasAlias := slices.ContainsFunc(player.Aliases, func(alias string) bool {
				return strings.EqualFold(alias, name)
			})
			if hasAlias {
				matches = append(matches, id)
			}
		}
		slices.Sort(matches)
		switch len(matches) {
		case 0:
			unmatched = append(unmatched, name)
		case 1:
			selected[matches[0]] = true
		default:
			return nil, &PlayerAliasAmbiguityError{Alias: name, SteamIDs: matches}
		}
	}
	if len(unmatched) > 0 {
		return nil, fmt.Errorf("players not found in series: %s; available players: %s",
			strings.Join(unmatched, ", "), strings.Join(seriesPlayerNames(series), ", "))
	}
	return selected, nil
}

// seriesPlayerNames lists every aggregate player for selection errors,
// sorted; a player the demos never named is listed by SteamID.
func seriesPlayerNames(series *SeriesAnalysis) []string {
	names := make([]string, 0, len(series.Players))
	for id, player := range series.Players {
		if player.Name != "" {
			names = append(names, player.Name)
		} else {
			names = append(names, strconv.FormatUint(id, 10))
		}
	}
	slices.Sort(names)
	return names
}

// FilterSeriesPlayers returns a copy of the series narrowed to the selected
// SteamIDs: the aggregate players and each map's standalone players are
// filtered, while teams, rosters, scores, map results and team assignments
// stay complete. The input series and its maps are not mutated.
func FilterSeriesPlayers(series *SeriesAnalysis, selected map[uint64]bool) *SeriesAnalysis {
	filtered := *series
	filtered.Players = make(map[uint64]*SeriesPlayer, len(selected))
	for id, player := range series.Players {
		if selected[id] {
			filtered.Players[id] = player
		}
	}
	filtered.Maps = make([]SeriesMap, len(series.Maps))
	for i, seriesMap := range series.Maps {
		analysis := *seriesMap.Analysis
		analysis.Players = make(map[uint64]*DemoPlayer, len(selected))
		for id, player := range seriesMap.Analysis.Players {
			if selected[id] {
				analysis.Players[id] = player
			}
		}
		seriesMap.Analysis = &analysis
		filtered.Maps[i] = seriesMap
	}
	return &filtered
}
