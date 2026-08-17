# Using the analysis engine as a Go library

Everything the CLI computes lives in the importable `analysis` package, which has no CLI, TUI, or rendering dependencies of its own. The generated API reference is available on [pkg.go.dev](https://pkg.go.dev/github.com/taua-almeida/cs2-analyser-tool/analysis).

```go
import "github.com/taua-almeida/cs2-analyser-tool/analysis"
```

Add the module to your project with `go get github.com/taua-almeida/cs2-analyser-tool@latest`.

## Analysing one map

`Analyse` reads a complete demo from any `io.Reader`, such as a file, uploaded stream, in-memory buffer, or object-storage reader, and returns the map's players, logical teams, map data, and game mode:

```go
result, err := analysis.Analyse(ctx, reader)
```

`AnalyseFile` is the file convenience wrapper. It owns the file it opens: open failures name the path, and the file is always closed before returning.

```go
result, err := analysis.AnalyseFile(ctx, "match.dem")
```

- **Input ownership**: `Analyse` borrows the reader and never closes it, even when it is an `io.Closer`. No rewindability is assumed and none survives a call. After success, failure, or cancellation the reader has been consumed to an unspecified position, so reusing it requires rewinding or reopening it first.
- **Stream failures**: A failing reader is an error, never a panic. An empty stream's immediate `io.EOF` or a connection dropped mid-demo comes back wrapping the reader's own error, so `errors.Is` still sees its identity.
- **Cancellation**: Cancelling the context interrupts parsing at the next demo frame instead of being checked only before or after the whole demo. It also unblocks a `Read` currently blocked inside the reader, so a stalled upload or network stream cannot pin the call. The returned error wraps the context's error, so `errors.Is(err, context.Canceled)` and `errors.Is(err, context.DeadlineExceeded)` hold as appropriate. A `Read` abandoned by cancellation may still be running inside the reader when `Analyse` returns; its result is discarded, and the reader stays yours to close if that read itself needs releasing.
- **Team identity**: Each map reports two logical teams, the lineups that persist through side swaps, whose IDs are map-local (team 1 played CT in the seeding round). A series numbers its own teams 1 and 2 series-locally, and each series map carries the explicit map-local-to-series translation in `team_assignments`. The two scopes never mix implicitly.

## Building a series

`BuildSeries` aggregates a completed, explicitly stated BO3 or BO5 from maps parsed with `Analyse` or `AnalyseFile`, supplied in played order with each demo's SHA-256 content digest:

```go
series, err := analysis.BuildSeries(3, []analysis.SeriesMapInput{
    {Demo: mapOne, SHA256: digestOne},
    {Demo: mapTwo, SHA256: digestTwo},
})
```

It is pure aggregation with no I/O and never mutates its inputs. It enforces the completed-series rule: the final supplied map must be the clinching one. When the two series teams cannot be resolved from rosters alone, the structured `SeriesTeamConflictError` and `SeriesTeamAmbiguityError` carry the competing evidence and match through `errors.As`.

## JSON contract and v0.x API stability

Every result type marshals with `encoding/json` into the same envelopes the CLI's `--save` writes, documented in [Player data](./PLAYER_DATA.MD). `MapAnalysis` is the standalone map document, while `SeriesAnalysis` is the series document embedding each map's unchanged analysis. A series player whose rating cannot be recomputed from raw per-map facts marshals it as `null` rather than a fabricated value.

While releases remain `v0.x`, the Go API is not frozen. Exported names and fields may change between minor versions as the surface settles. The JSON contracts are the stable part. You can also inspect the installed package with `go doc github.com/taua-almeida/cs2-analyser-tool/analysis`.
