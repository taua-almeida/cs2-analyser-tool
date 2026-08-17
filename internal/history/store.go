package history

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"time"

	"github.com/taua-almeida/cs2-analyser-tool/analysis"
)

// StoreMatchInput is everything one successfully analysed map stores.
type StoreMatchInput struct {
	// SHA256 is the lowercase hex digest of the exact demo bytes that were
	// parsed. It is the match's identity: storing the same digest again
	// never rewrites the canonical row.
	SHA256 string
	// AnalysedAt is when the analysis ran — never when the match was
	// played, which no demo records trustworthily. The caller supplies it
	// so tests can inject a clock; it is stored in UTC.
	AnalysedAt time.Time
	// AnalysisVersion is the cs2-analyser-tool version that produced the
	// analysis.
	AnalysisVersion string
	// Analysis is the complete, unfiltered map analysis. Player display
	// selection never narrows it; that lives in SelectedSteamIDs.
	Analysis *analysis.MapAnalysis
	// Facts are the exact per-player aggregation facts, keyed by the same
	// SteamIDs as Analysis.Players; a mismatch either way is rejected.
	Facts map[uint64]analysis.PlayerAggregationFacts
	// SelectedSteamIDs is the display preference to store: the players the
	// user chose to see. nil means no explicit selection — display
	// everyone. A non-nil slice is an explicit selection even when, after
	// narrowing to the players actually present in this map, nobody is
	// left: a series selection of players who all sat this map out stays
	// an empty view, exactly as the live series rendering shows it. IDs
	// are sorted and deduplicated.
	SelectedSteamIDs []uint64
}

// StoreResult reports what StoreMatch did.
type StoreResult struct {
	// Created is true when this call inserted the canonical match, false
	// when the digest was already stored and only the display preference
	// was replaced.
	Created bool
}

// StoreMatch stores one analysed map — canonical row, players, facts and
// display preference — in a single transaction. The first store of a digest
// creates the complete match; any later store of the same digest leaves the
// canonical data and its analysis timestamp untouched and only replaces the
// display preference. Any failure rolls the whole attempt back, leaving
// previously stored history unchanged.
func (db *DB) StoreMatch(ctx context.Context, input StoreMatchInput) (StoreResult, error) {
	if err := validateDigest(input.SHA256); err != nil {
		return StoreResult{}, err
	}
	if input.Analysis == nil {
		return StoreResult{}, errors.New("storing match: analysis is nil")
	}
	if err := validateFactsCoverPlayers(input.Analysis.Players, input.Facts); err != nil {
		return StoreResult{}, err
	}
	selection, err := normalizeSelection(input.SelectedSteamIDs)
	if err != nil {
		return StoreResult{}, err
	}
	analysisJSON, err := json.Marshal(input.Analysis)
	if err != nil {
		return StoreResult{}, fmt.Errorf("encoding analysis for match %s: %w", input.SHA256, err)
	}

	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return StoreResult{}, fmt.Errorf("beginning history transaction: %w", err)
	}
	defer tx.Rollback()

	// DO NOTHING is deliberate and narrow: an existing digest means the
	// canonical match — same content, same first-analysis timestamp — must
	// survive unchanged. Every other conflict in this transaction is a real
	// error and rolls everything back.
	res, err := tx.ExecContext(ctx, `
		INSERT INTO matches
			(demo_sha256, analysed_at, analysis_version, map_name, game_mode,
			 score_kind, score_a, score_b, analysis_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (demo_sha256) DO NOTHING`,
		input.SHA256,
		input.AnalysedAt.UTC().Format(time.RFC3339Nano),
		input.AnalysisVersion,
		input.Analysis.Map.MapName,
		input.Analysis.GameMode,
		string(scoreKindOf(input.Analysis)),
		scoreAOf(input.Analysis),
		scoreBOf(input.Analysis),
		analysisJSON,
	)
	if err != nil {
		return StoreResult{}, fmt.Errorf("inserting match %s: %w", input.SHA256, err)
	}
	inserted, err := res.RowsAffected()
	if err != nil {
		return StoreResult{}, fmt.Errorf("inserting match %s: %w", input.SHA256, err)
	}
	created := inserted > 0
	if created {
		if err := insertMatchPlayers(ctx, tx, input); err != nil {
			return StoreResult{}, err
		}
	}
	if err := replacePreferences(ctx, tx, input.SHA256, selection); err != nil {
		return StoreResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return StoreResult{}, fmt.Errorf("committing match %s: %w", input.SHA256, err)
	}
	return StoreResult{Created: created}, nil
}

// insertMatchPlayers stores every player of a newly created match with the
// alias observed in this map, the complete DemoPlayer, and the exact
// aggregation facts. It runs inside the match's transaction, so any invalid
// player — SteamID zero, an inconsistent ID, a value JSON cannot encode —
// rolls back the whole new match before commit.
func insertMatchPlayers(ctx context.Context, tx *sql.Tx, input StoreMatchInput) error {
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO match_players (match_sha256, steam_id, alias, player_json, facts_json)
		VALUES (?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("preparing player insert: %w", err)
	}
	defer stmt.Close()

	for _, id := range slices.Sorted(maps.Keys(input.Analysis.Players)) {
		player := input.Analysis.Players[id]
		if id == 0 || player == nil {
			return fmt.Errorf("match %s has an invalid player entry for SteamID %d", input.SHA256, id)
		}
		if player.SteamID != id {
			return fmt.Errorf("match %s player keyed %d reports SteamID %d", input.SHA256, id, player.SteamID)
		}
		playerJSON, err := json.Marshal(player)
		if err != nil {
			return fmt.Errorf("encoding player %d of match %s: %w", id, input.SHA256, err)
		}
		factsJSON, err := json.Marshal(input.Facts[id])
		if err != nil {
			return fmt.Errorf("encoding facts of player %d of match %s: %w", id, input.SHA256, err)
		}
		if _, err := stmt.ExecContext(ctx, input.SHA256,
			strconv.FormatUint(id, 10), player.Name, playerJSON, factsJSON); err != nil {
			return fmt.Errorf("inserting player %d of match %s: %w", id, input.SHA256, err)
		}
	}
	return nil
}

// replacePreferences replaces the stored display selection of one match: a
// nil selection stores the display-everyone state, a non-nil one records
// that a selection was explicitly made plus its IDs narrowed to the players
// actually stored for this map — a series-wide selection legitimately names
// players who sat this map out, and one that keeps nobody stays an explicit
// empty view. It never touches the canonical match or player rows.
func replacePreferences(ctx context.Context, tx *sql.Tx, sha256 string, selection []uint64) error {
	if _, err := tx.ExecContext(ctx,
		"DELETE FROM display_preferences WHERE match_sha256 = ?", sha256); err != nil {
		return fmt.Errorf("clearing display preference of match %s: %w", sha256, err)
	}
	if _, err := tx.ExecContext(ctx,
		"DELETE FROM display_selections WHERE match_sha256 = ?", sha256); err != nil {
		return fmt.Errorf("clearing display selection of match %s: %w", sha256, err)
	}
	if selection == nil {
		return nil
	}
	if _, err := tx.ExecContext(ctx,
		"INSERT INTO display_selections (match_sha256) VALUES (?)", sha256); err != nil {
		return fmt.Errorf("storing display selection of match %s: %w", sha256, err)
	}
	present, err := storedPlayerIDs(ctx, tx, sha256)
	if err != nil {
		return err
	}
	for _, id := range selection {
		if !present[id] {
			continue
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO display_preferences (match_sha256, steam_id)
			VALUES (?, ?)`, sha256, strconv.FormatUint(id, 10)); err != nil {
			return fmt.Errorf("storing display preference %d of match %s: %w", id, sha256, err)
		}
	}
	return nil
}

// storedPlayerIDs reads the SteamIDs stored for one match inside the running
// transaction, so preferences are always checked against the canonical rows
// rather than this call's input.
func storedPlayerIDs(ctx context.Context, tx *sql.Tx, sha256 string) (map[uint64]bool, error) {
	rows, err := tx.QueryContext(ctx,
		"SELECT steam_id FROM match_players WHERE match_sha256 = ?", sha256)
	if err != nil {
		return nil, fmt.Errorf("reading players of match %s: %w", sha256, err)
	}
	defer rows.Close()

	present := make(map[uint64]bool)
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, fmt.Errorf("reading players of match %s: %w", sha256, err)
		}
		id, err := parseSteamID(raw)
		if err != nil {
			return nil, fmt.Errorf("match %s: %w", sha256, err)
		}
		present[id] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading players of match %s: %w", sha256, err)
	}
	return present, nil
}

// validateDigest requires the exact stored digest shape: 64 lowercase
// hexadecimal characters.
func validateDigest(digest string) error {
	if len(digest) != 64 {
		return fmt.Errorf("demo digest %q is %d characters, want 64 lowercase hex", digest, len(digest))
	}
	for _, r := range digest {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return fmt.Errorf("demo digest %q is not lowercase hex", digest)
		}
	}
	return nil
}

// validateFactsCoverPlayers requires the facts and player maps to carry
// exactly the same SteamIDs, so a stored match can never hold a player
// without exact facts or facts for a phantom player.
func validateFactsCoverPlayers(players map[uint64]*analysis.DemoPlayer, facts map[uint64]analysis.PlayerAggregationFacts) error {
	var missing, extra []uint64
	for id := range players {
		if _, ok := facts[id]; !ok {
			missing = append(missing, id)
		}
	}
	for id := range facts {
		if _, ok := players[id]; !ok {
			extra = append(extra, id)
		}
	}
	if len(missing) == 0 && len(extra) == 0 {
		return nil
	}
	slices.Sort(missing)
	slices.Sort(extra)
	return fmt.Errorf("aggregation facts do not match the map's players: players without facts %v, facts without players %v", missing, extra)
}

// normalizeSelection sorts and deduplicates a display selection and rejects
// SteamID zero, which can never name a stored player. Nil-ness is preserved:
// nil stays nil (no explicit selection) and an empty non-nil selection stays
// non-nil (an explicit selection of nobody).
func normalizeSelection(ids []uint64) ([]uint64, error) {
	selection := slices.Clone(ids)
	slices.Sort(selection)
	selection = slices.Compact(selection)
	if len(selection) > 0 && selection[0] == 0 {
		return nil, errors.New("display preference SteamID 0 is invalid")
	}
	return selection, nil
}

// parseSteamID parses the canonical stored SteamID64 form: nonzero unsigned
// decimal text.
func parseSteamID(raw string) (uint64, error) {
	id, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("stored SteamID %q is not unsigned decimal: %w", raw, err)
	}
	if id == 0 {
		return 0, errors.New("stored SteamID is zero")
	}
	return id, nil
}

// ScoreKind says which scores a stored match carries.
type ScoreKind string

const (
	// ScoreKindTeams is the preferred kind: the two logical teams' round
	// wins, in team order.
	ScoreKindTeams ScoreKind = "teams"
	// ScoreKindSides is the fallback when a map resolved no logical teams:
	// the final CT and T side scores.
	ScoreKindSides ScoreKind = "sides"
)

func scoreKindOf(demo *analysis.MapAnalysis) ScoreKind {
	if len(demo.Teams) == 2 {
		return ScoreKindTeams
	}
	return ScoreKindSides
}

func scoreAOf(demo *analysis.MapAnalysis) int {
	if len(demo.Teams) == 2 {
		return demo.Teams[0].Score
	}
	return demo.Map.RoundsWonCT
}

func scoreBOf(demo *analysis.MapAnalysis) int {
	if len(demo.Teams) == 2 {
		return demo.Teams[1].Score
	}
	return demo.Map.RoundsWonT
}
