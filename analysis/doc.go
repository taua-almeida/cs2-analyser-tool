// Package analysis parses Counter-Strike 2 demos and aggregates completed
// best-of-3 and best-of-5 series, exposing both as plain Go values. It is the
// engine behind the cs2-analyser-tool CLI and is importable on its own: it never
// renders, prompts, writes files or exits the process.
//
// # Analysing a map
//
// [Analyse] reads one complete demo from an [io.Reader] — a file, an
// uploaded stream, an in-memory buffer, an object-storage reader — and
// returns a [MapAnalysis] with per-player statistics, the map's data and its
// two logical teams. [AnalyseFile] is the file convenience wrapper: it opens
// the path, analyses it, and always closes the file itself.
//
//	result, err := analysis.Analyse(ctx, reader)
//
// Analyse borrows the reader and never closes it; the caller keeps
// ownership. No rewindability is assumed and none survives a call: after
// success, failure or cancellation the reader has been consumed to an
// unspecified position, so reuse requires rewinding or reopening it. A
// failing reader — an empty stream's immediate io.EOF, a connection dropped
// mid-demo — is returned as an error wrapping the reader's own, never a
// panic.
//
// Cancelling the context interrupts parsing at the next demo frame rather
// than being checked only before or after the demo, and it also unblocks a
// Read currently blocked inside the reader, so a stalled stream cannot pin
// the call. The returned error wraps the context's error, so
// errors.Is(err, context.Canceled) and
// errors.Is(err, context.DeadlineExceeded) hold as appropriate. A Read
// abandoned by cancellation may still be running inside the reader when
// Analyse returns; its result is discarded.
//
// # Team identity
//
// CT and T are temporary sides that swap at halftime; a logical team is the
// lineup that persists across those swaps. [MapAnalysis] reports two logical
// [DemoTeam] values whose TeamID is map-local: team 1 played CT in the
// seeding round, team 2 played T, and the IDs claim no identity beyond that
// one map. [SeriesAnalysis] likewise numbers its [SeriesTeam] values 1 and 2
// series-locally, and each [SeriesMap] carries the explicit
// [SeriesTeamAssignment] pairs translating its map-local team IDs into
// series team IDs. Clan names are labels, never identity.
//
// # Building a series
//
// [BuildSeries] aggregates a completed, caller-stated best-of-3 or best-of-5
// from maps parsed with [Analyse] or [AnalyseFile], supplied in played
// order:
//
//	series, err := analysis.BuildSeries(3, []analysis.SeriesMapInput{
//		{Demo: mapOne, SHA256: digestOne},
//		{Demo: mapTwo, SHA256: digestTwo},
//	})
//
// It is pure aggregation — no I/O — and never mutates its inputs. The series
// must be clinched exactly on the final supplied map. When the two series
// teams cannot be resolved from the rosters alone, the structured
// [SeriesTeamConflictError] and [SeriesTeamAmbiguityError] carry the
// competing evidence and remain matchable through errors.As.
//
// # JSON
//
// Every result type marshals with encoding/json into the same envelope the
// CLI's --save json writes: [MapAnalysis] is the standalone map document and
// [SeriesAnalysis] the series document embedding each map's unchanged
// analysis. A [SeriesPlayer] whose rating could not be recomputed from raw
// facts marshals it as null rather than a fabricated value.
//
// # Stability
//
// While the module's releases remain v0.x this API is not frozen: exported
// names, fields and behavior may still change between minor versions as the
// surface settles. The JSON contracts above are the stable part.
package analysis
