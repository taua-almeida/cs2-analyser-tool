package history

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/taua-almeida/cs2-analyser-tool/analysis"
)

// shortIDLen is the stable digest prefix length shown to users. Twelve hex
// characters cannot realistically collide in a personal history, and lookup
// accepts any unique prefix of at least minMatchIDLen anyway.
const shortIDLen = 12

// minMatchIDLen is the shortest accepted match ID prefix.
const minMatchIDLen = 8

// digestLen is the full SHA-256 hex length.
const digestLen = 64

// MatchSummary is one stored match's summary columns — everything the
// history listing shows, read without decoding the analysis blob.
type MatchSummary struct {
	// SHA256 is the full content digest; ShortID derives the display form.
	SHA256 string
	// AnalysedAt is the UTC analysis time: when the demo was parsed, not
	// when the match was played.
	AnalysedAt time.Time
	// AnalysisVersion is the cs2-analyser-tool version that analysed it.
	AnalysisVersion string
	MapName         string
	GameMode        string
	ScoreKind       ScoreKind
	ScoreA          int
	ScoreB          int
}

// ShortID is the stable 12-character digest prefix the CLI displays.
func (s MatchSummary) ShortID() string {
	return s.SHA256[:shortIDLen]
}

// Score formats the stored scores as "a:b".
func (s MatchSummary) Score() string {
	return fmt.Sprintf("%d:%d", s.ScoreA, s.ScoreB)
}

// StoredMatch is one complete stored match: its summary, the decoded
// unfiltered analysis, and the stored display selection.
type StoredMatch struct {
	MatchSummary
	Analysis *analysis.MapAnalysis
	// SelectionExplicit reports whether a display selection was explicitly
	// stored for this match. False means display everyone.
	SelectionExplicit bool
	// SelectedSteamIDs is the stored display preference, sorted ascending.
	// With SelectionExplicit it can be empty: an explicit selection whose
	// players all sat this map out keeps the view empty rather than
	// falling back to everyone.
	SelectedSteamIDs []uint64
}

// AmbiguousMatchIDError reports a prefix matching more than one stored
// match, carrying the candidates' full IDs.
type AmbiguousMatchIDError struct {
	Prefix     string
	Candidates []string // full digests, sorted ascending
}

func (e *AmbiguousMatchIDError) Error() string {
	return fmt.Sprintf("match ID prefix %q is ambiguous between %s; use more characters",
		e.Prefix, strings.Join(e.Candidates, ", "))
}

// ListMatches returns every stored match's summary, newest analysis first,
// timestamp ties broken by the full digest. It reads only summary columns —
// the analysis blobs stay untouched however large the history grows.
func (db *DB) ListMatches(ctx context.Context) ([]MatchSummary, error) {
	rows, err := db.sql.QueryContext(ctx, `
		SELECT demo_sha256, analysed_at, analysis_version, map_name, game_mode,
		       score_kind, score_a, score_b
		FROM matches`)
	if err != nil {
		return nil, fmt.Errorf("listing history: %w", err)
	}
	defer rows.Close()

	var matches []MatchSummary
	for rows.Next() {
		summary, err := scanSummary(rows)
		if err != nil {
			return nil, fmt.Errorf("listing history: %w", err)
		}
		matches = append(matches, summary)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("listing history: %w", err)
	}
	// RFC3339Nano trims trailing zeros, so the text column does not order
	// chronologically in SQL; the parsed times do.
	slices.SortFunc(matches, func(a, b MatchSummary) int {
		if c := b.AnalysedAt.Compare(a.AnalysedAt); c != 0 {
			return c
		}
		return cmp.Compare(a.SHA256, b.SHA256)
	})
	return matches, nil
}

// summaryRow abstracts sql.Row and sql.Rows scanning for the shared summary
// columns.
type summaryRow interface {
	Scan(dest ...any) error
}

func scanSummary(row summaryRow) (MatchSummary, error) {
	var summary MatchSummary
	var analysedAt, kind string
	if err := row.Scan(&summary.SHA256, &analysedAt, &summary.AnalysisVersion,
		&summary.MapName, &summary.GameMode, &kind, &summary.ScoreA, &summary.ScoreB); err != nil {
		return MatchSummary{}, err
	}
	parsed, err := parseAnalysedAt(summary.SHA256, analysedAt)
	if err != nil {
		return MatchSummary{}, err
	}
	summary.AnalysedAt = parsed
	summary.ScoreKind = ScoreKind(kind)
	return summary, nil
}

// MatchByID returns the complete stored match named by a full digest or a
// unique hexadecimal prefix of at least eight characters, decoding the
// stored analysis — the original demo is not involved. An ambiguous prefix
// fails with the candidate IDs.
func (db *DB) MatchByID(ctx context.Context, id string) (*StoredMatch, error) {
	digest, err := db.resolveMatchID(ctx, id)
	if err != nil {
		return nil, err
	}

	row := db.sql.QueryRowContext(ctx, `
		SELECT demo_sha256, analysed_at, analysis_version, map_name, game_mode,
		       score_kind, score_a, score_b, analysis_json
		FROM matches WHERE demo_sha256 = ?`, digest)
	var summary MatchSummary
	var analysedAt, kind string
	var analysisJSON []byte
	err = row.Scan(&summary.SHA256, &analysedAt, &summary.AnalysisVersion,
		&summary.MapName, &summary.GameMode, &kind, &summary.ScoreA, &summary.ScoreB,
		&analysisJSON)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("no stored match has ID %s", digest)
	}
	if err != nil {
		return nil, fmt.Errorf("reading match %s: %w", digest, err)
	}
	parsed, err := parseAnalysedAt(digest, analysedAt)
	if err != nil {
		return nil, err
	}
	summary.AnalysedAt = parsed
	summary.ScoreKind = ScoreKind(kind)

	var demo analysis.MapAnalysis
	if err := json.Unmarshal(analysisJSON, &demo); err != nil {
		return nil, fmt.Errorf("decoding stored analysis of match %s: %w", digest, err)
	}
	explicit, selected, err := db.displaySelection(ctx, digest)
	if err != nil {
		return nil, err
	}
	return &StoredMatch{
		MatchSummary:      summary,
		Analysis:          &demo,
		SelectionExplicit: explicit,
		SelectedSteamIDs:  selected,
	}, nil
}

// resolveMatchID normalizes and resolves a user-supplied match ID: a full
// 64-character digest is used as is, and a shorter hexadecimal prefix of at
// least eight characters must match exactly one stored match.
func (db *DB) resolveMatchID(ctx context.Context, id string) (string, error) {
	normalized := strings.ToLower(id)
	if err := validateMatchIDPrefix(normalized); err != nil {
		return "", err
	}
	if len(normalized) == digestLen {
		return normalized, nil
	}
	// The prefix is validated hexadecimal, so it cannot smuggle LIKE
	// wildcards into the pattern.
	rows, err := db.sql.QueryContext(ctx,
		"SELECT demo_sha256 FROM matches WHERE demo_sha256 LIKE ? ORDER BY demo_sha256",
		normalized+"%")
	if err != nil {
		return "", fmt.Errorf("resolving match ID %q: %w", id, err)
	}
	defer rows.Close()

	var candidates []string
	for rows.Next() {
		var digest string
		if err := rows.Scan(&digest); err != nil {
			return "", fmt.Errorf("resolving match ID %q: %w", id, err)
		}
		candidates = append(candidates, digest)
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("resolving match ID %q: %w", id, err)
	}
	switch len(candidates) {
	case 0:
		return "", fmt.Errorf("no stored match has ID %s", normalized)
	case 1:
		return candidates[0], nil
	default:
		return "", &AmbiguousMatchIDError{Prefix: normalized, Candidates: candidates}
	}
}

func validateMatchIDPrefix(id string) error {
	if len(id) < minMatchIDLen {
		return fmt.Errorf("match ID %q is too short: use at least %d characters of the digest", id, minMatchIDLen)
	}
	if len(id) > digestLen {
		return fmt.Errorf("match ID %q is longer than a SHA-256 digest", id)
	}
	for _, r := range id {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return fmt.Errorf("match ID %q is not hexadecimal", id)
		}
	}
	return nil
}

// displaySelection reads one match's stored display selection: whether one
// was explicitly made, and the selected SteamIDs sorted ascending. Not
// explicit means display everyone.
func (db *DB) displaySelection(ctx context.Context, digest string) (bool, []uint64, error) {
	var explicit bool
	err := db.sql.QueryRowContext(ctx,
		"SELECT count(*) > 0 FROM display_selections WHERE match_sha256 = ?", digest).Scan(&explicit)
	if err != nil {
		return false, nil, fmt.Errorf("reading display selection of match %s: %w", digest, err)
	}

	rows, err := db.sql.QueryContext(ctx,
		"SELECT steam_id FROM display_preferences WHERE match_sha256 = ?", digest)
	if err != nil {
		return false, nil, fmt.Errorf("reading display preference of match %s: %w", digest, err)
	}
	defer rows.Close()

	var selected []uint64
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return false, nil, fmt.Errorf("reading display preference of match %s: %w", digest, err)
		}
		id, err := parseSteamID(raw)
		if err != nil {
			return false, nil, fmt.Errorf("match %s: %w", digest, err)
		}
		selected = append(selected, id)
	}
	if err := rows.Err(); err != nil {
		return false, nil, fmt.Errorf("reading display preference of match %s: %w", digest, err)
	}
	slices.Sort(selected)
	return explicit, selected, nil
}
