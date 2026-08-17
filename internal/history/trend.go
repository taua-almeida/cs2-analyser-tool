package history

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/taua-almeida/cs2-analyser-tool/analysis"
)

// premierGameMode is the stored game_mode a match must report to be part of
// a Premier trend. Nothing is inferred: a match either recorded the mode or
// it is excluded.
const premierGameMode = "premier"

// AliasCandidate is one stored player an alias could mean.
type AliasCandidate struct {
	SteamID uint64
	// Aliases are every distinct alias stored for this SteamID, sorted.
	Aliases []string
}

// AliasAmbiguityError reports an alias matching more than one stored
// SteamID, carrying each candidate's ID and known aliases.
type AliasAmbiguityError struct {
	Alias      string
	Candidates []AliasCandidate
}

func (e *AliasAmbiguityError) Error() string {
	parts := make([]string, len(e.Candidates))
	for i, candidate := range e.Candidates {
		parts[i] = fmt.Sprintf("%d (known as: %s)", candidate.SteamID, strings.Join(candidate.Aliases, ", "))
	}
	return fmt.Sprintf("player %q matches more than one stored SteamID: %s; pass the SteamID64 instead",
		e.Alias, strings.Join(parts, "; "))
}

// ResolvePlayer turns a --player value into a SteamID64. A nonzero decimal
// SteamID64 is taken literally; anything else must equal exactly one stored
// player's alias under Unicode simple case folding, the strings.EqualFold
// semantics the storage contract specifies: one-to-one case pairs match
// (Ö/ö, Cyrillic И/и), multi-rune expansions do not (ß never matches "ss").
// SQLite's NOCASE, which folds only ASCII, is deliberately not used.
// Aliases accumulate across all stored maps. An alias shared by several
// SteamIDs fails with an AliasAmbiguityError naming every candidate.
func (db *DB) ResolvePlayer(ctx context.Context, player string) (uint64, error) {
	if id, err := strconv.ParseUint(player, 10, 64); err == nil && id != 0 {
		return id, nil
	}
	known, err := db.playerAliases(ctx)
	if err != nil {
		return 0, err
	}
	var candidates []AliasCandidate
	for _, candidate := range known {
		matches := slices.ContainsFunc(candidate.Aliases, func(alias string) bool {
			return strings.EqualFold(alias, player)
		})
		if matches {
			candidates = append(candidates, candidate)
		}
	}
	switch len(candidates) {
	case 0:
		return 0, fmt.Errorf("no stored player is named %q; pass a SteamID64 or an alias from a stored match", player)
	case 1:
		return candidates[0].SteamID, nil
	default:
		return 0, &AliasAmbiguityError{Alias: player, Candidates: candidates}
	}
}

// playerAliases collects every stored player with their distinct aliases,
// sorted by SteamID then alias for deterministic errors.
func (db *DB) playerAliases(ctx context.Context) ([]AliasCandidate, error) {
	rows, err := db.sql.QueryContext(ctx,
		"SELECT DISTINCT steam_id, alias FROM match_players ORDER BY steam_id, alias")
	if err != nil {
		return nil, fmt.Errorf("reading stored aliases: %w", err)
	}
	defer rows.Close()

	byID := make(map[uint64]*AliasCandidate)
	var order []uint64
	for rows.Next() {
		var raw, alias string
		if err := rows.Scan(&raw, &alias); err != nil {
			return nil, fmt.Errorf("reading stored aliases: %w", err)
		}
		id, err := parseSteamID(raw)
		if err != nil {
			return nil, err
		}
		candidate := byID[id]
		if candidate == nil {
			candidate = &AliasCandidate{SteamID: id}
			byID[id] = candidate
			order = append(order, id)
		}
		candidate.Aliases = append(candidate.Aliases, alias)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading stored aliases: %w", err)
	}
	slices.Sort(order)
	candidates := make([]AliasCandidate, 0, len(order))
	for _, id := range order {
		candidates = append(candidates, *byID[id])
	}
	return candidates, nil
}

// TrendMatch is one stored Premier map a player participated in, with the
// alias observed there, that map's round count, the complete stored
// DemoPlayer, and the exact aggregation facts.
type TrendMatch struct {
	MatchSummary
	Alias  string
	Rounds int
	Player analysis.DemoPlayer
	Facts  analysis.PlayerAggregationFacts
}

// TrendTotals are the additive sums a trend's aggregate rates derive from.
// Every field is an exact sum over the included maps; no displayed
// percentage, ratio or rating is ever averaged.
type TrendTotals struct {
	Maps   int
	Rounds int // summed map rounds — every included map's full round count, matching per-map ADR/KAST denominators
	Kills  int
	Deaths int
	// Headshots counts headshot kills, the HS% numerator.
	Headshots   int
	DamageGiven int
	// KASTRounds counts rounds with classic KAST credit, from stored facts.
	KASTRounds int
	// OpeningRoundsWon counts rounds won after taking the opening kill,
	// from stored facts.
	OpeningRoundsWon      int
	OpeningKills          int
	OpeningDeaths         int
	TradeKills            int
	DeathsTraded          int
	UtilityDamage         int
	EnemiesFlashed        int
	EnemyFlashTimeSeconds float64
	GrenadesThrown        int
	UnusedUtilityValue    int
}

// KD is sum(kills) / sum(deaths). Zero deaths divide by one instead — the
// same convention the per-map K/D column uses — so a deathless run reads as
// its kill count.
func (t TrendTotals) KD() float64 {
	deaths := t.Deaths
	if deaths == 0 {
		deaths = 1
	}
	return float64(t.Kills) / float64(deaths)
}

// ADR is sum(damage given) / sum(map rounds); 0 with no rounds.
func (t TrendTotals) ADR() float64 {
	if t.Rounds == 0 {
		return 0
	}
	return float64(t.DamageGiven) / float64(t.Rounds)
}

// KASTPercent is 100 * sum(KAST credited rounds) / sum(map rounds); 0 with
// no rounds.
func (t TrendTotals) KASTPercent() float64 {
	if t.Rounds == 0 {
		return 0
	}
	return 100 * float64(t.KASTRounds) / float64(t.Rounds)
}

// HSPercent is 100 * sum(headshots) / sum(kills); 0 with no kills.
func (t TrendTotals) HSPercent() float64 {
	if t.Kills == 0 {
		return 0
	}
	return 100 * float64(t.Headshots) / float64(t.Kills)
}

// OpeningSuccessPercent is 100 * sum(opening rounds won) / sum(opening
// kills); 0 with no opening kills.
func (t TrendTotals) OpeningSuccessPercent() float64 {
	if t.OpeningKills == 0 {
		return 0
	}
	return 100 * float64(t.OpeningRoundsWon) / float64(t.OpeningKills)
}

// PlayerTrend is one player's Premier trend: the maps they appear in,
// chronological by analysis time, and the exact additive totals.
type PlayerTrend struct {
	SteamID uint64
	// Aliases are the distinct aliases observed in the included maps, in
	// first-observation order.
	Aliases []string
	Matches []TrendMatch
	Totals  TrendTotals
}

// PremierTrend builds the trend for one SteamID from stored matches only:
// maps explicitly recorded as premier that this player participated in.
// Series aggregates are never stored, so nothing here can double count. An
// unknown or trendless SteamID returns an empty trend, not an error.
func (db *DB) PremierTrend(ctx context.Context, steamID uint64) (*PlayerTrend, error) {
	if steamID == 0 {
		return nil, fmt.Errorf("SteamID 0 is invalid")
	}
	rows, err := db.sql.QueryContext(ctx, `
		SELECT m.demo_sha256, m.analysed_at, m.analysis_version, m.map_name,
		       m.game_mode, m.score_kind, m.score_a, m.score_b,
		       p.alias, p.player_json, p.facts_json, m.analysis_json
		FROM matches m
		JOIN match_players p ON p.match_sha256 = m.demo_sha256
		WHERE p.steam_id = ? AND m.game_mode = ?`,
		strconv.FormatUint(steamID, 10), premierGameMode)
	if err != nil {
		return nil, fmt.Errorf("reading premier matches of %d: %w", steamID, err)
	}
	defer rows.Close()

	trend := &PlayerTrend{SteamID: steamID}
	for rows.Next() {
		match, err := scanTrendMatch(rows)
		if err != nil {
			return nil, fmt.Errorf("reading premier matches of %d: %w", steamID, err)
		}
		trend.Matches = append(trend.Matches, match)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading premier matches of %d: %w", steamID, err)
	}

	slices.SortFunc(trend.Matches, func(a, b TrendMatch) int {
		if c := a.AnalysedAt.Compare(b.AnalysedAt); c != 0 {
			return c
		}
		return cmp.Compare(a.SHA256, b.SHA256)
	})
	for _, match := range trend.Matches {
		if match.Alias != "" && !slices.Contains(trend.Aliases, match.Alias) {
			trend.Aliases = append(trend.Aliases, match.Alias)
		}
		trend.Totals.add(match)
	}
	return trend, nil
}

func scanTrendMatch(rows summaryRow) (TrendMatch, error) {
	var match TrendMatch
	var playerJSON, factsJSON, analysisJSON []byte
	var analysedAt, kind string
	if err := rows.Scan(&match.SHA256, &analysedAt, &match.AnalysisVersion,
		&match.MapName, &match.GameMode, &kind, &match.ScoreA, &match.ScoreB,
		&match.Alias, &playerJSON, &factsJSON, &analysisJSON); err != nil {
		return TrendMatch{}, err
	}
	parsed, err := parseAnalysedAt(match.SHA256, analysedAt)
	if err != nil {
		return TrendMatch{}, err
	}
	match.AnalysedAt = parsed
	match.ScoreKind = ScoreKind(kind)
	if err := json.Unmarshal(playerJSON, &match.Player); err != nil {
		return TrendMatch{}, fmt.Errorf("decoding stored player of match %s: %w", match.SHA256, err)
	}
	if err := json.Unmarshal(factsJSON, &match.Facts); err != nil {
		return TrendMatch{}, fmt.Errorf("decoding stored facts of match %s: %w", match.SHA256, err)
	}
	// Only map_data is needed here; the envelope's other keys are skipped
	// rather than materialized.
	var envelope struct {
		Map analysis.MapData `json:"map_data"`
	}
	if err := json.Unmarshal(analysisJSON, &envelope); err != nil {
		return TrendMatch{}, fmt.Errorf("decoding stored analysis of match %s: %w", match.SHA256, err)
	}
	match.Rounds = envelope.Map.TotalRounds
	return match, nil
}

// add folds one map's additive facts into the totals. Counts come from the
// stored DemoPlayer, KAST credits and opening wins from the exact
// aggregation facts.
func (t *TrendTotals) add(match TrendMatch) {
	player := match.Player
	t.Maps++
	t.Rounds += match.Rounds
	t.Kills += player.KillStats.Total
	t.Deaths += player.Deaths
	t.Headshots += player.KillStats.HeadShots
	t.DamageGiven += player.AssistStats.DamageGiven
	t.KASTRounds += match.Facts.KASTRounds.Total
	t.OpeningRoundsWon += match.Facts.OpeningRoundsWon
	t.OpeningKills += player.OpeningDuelStats.OpeningKills.Total
	t.OpeningDeaths += player.OpeningDuelStats.OpeningDeaths.Total
	t.TradeKills += player.KillStats.TradeKills
	t.DeathsTraded += player.DeathsTraded.Total
	t.UtilityDamage += player.UtilityStats.UtilityDamage.Total
	t.EnemiesFlashed += player.UtilityStats.EnemiesFlashed
	t.EnemyFlashTimeSeconds += player.UtilityStats.EnemyFlashTimeSeconds
	t.GrenadesThrown += player.UtilityStats.GrenadesThrown.Total
	t.UnusedUtilityValue += player.UtilityStats.UnusedUtilityValue
}

// parseAnalysedAt parses the stored UTC RFC3339Nano analysis time.
func parseAnalysedAt(digest, raw string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}, fmt.Errorf("match %s has invalid analysed_at %q: %w", digest, raw, err)
	}
	return parsed, nil
}
