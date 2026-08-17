# cs2-analyser-tool

Designed specifically for players and coaches, this command-line interface tool provides a simple display and easy data ready to analyse, compare and start your journey towards improving your team and your personal CS2 skills.

## Installation

### Prebuilt archives

Prebuilt archives are published for each release from `v0.1.0` onward at [GitHub Releases](https://github.com/taua-almeida/cs2-analyser-tool/releases). Download `checksums.txt` and the archive for your system from the same release:

| System | Archive |
| --- | --- |
| Linux, 64-bit Intel/AMD | `cs2-analyser-tool-linux-amd64.tar.gz` |
| Windows, 64-bit Intel/AMD | `cs2-analyser-tool-windows-amd64.zip` |
| macOS, Intel | `cs2-analyser-tool-darwin-amd64.tar.gz` |
| macOS, Apple silicon | `cs2-analyser-tool-darwin-arm64.tar.gz` |

Verify the downloaded archive before extracting it. On Linux, replace the archive name if needed:

```sh
grep 'cs2-analyser-tool-linux-amd64.tar.gz$' checksums.txt | sha256sum --check -
```

On macOS:

```sh
grep 'cs2-analyser-tool-darwin-arm64.tar.gz$' checksums.txt | shasum -a 256 --check -
```

On Windows PowerShell:

```powershell
$archive = "cs2-analyser-tool-windows-amd64.zip"
$expected = ((Get-Content checksums.txt | Where-Object { $_ -match ([regex]::Escape($archive) + '$') }) -split '\s+')[0]
$actual = (Get-FileHash $archive -Algorithm SHA256).Hash.ToLowerInvariant()
if ($actual -ne $expected) { throw "checksum mismatch" }
```

Extract a Linux or macOS archive into its own directory and run the binary:

```sh
mkdir cs2-analyser-tool-release
tar -xzf cs2-analyser-tool-linux-amd64.tar.gz -C cs2-analyser-tool-release
./cs2-analyser-tool-release/cs2-analyser-tool version
```

For Windows:

```powershell
Expand-Archive cs2-analyser-tool-windows-amd64.zip -DestinationPath cs2-analyser-tool-release
.\cs2-analyser-tool-release\cs2-analyser-tool.exe version
```

Each archive also contains `README.md` and `LICENSE`.

### Install with Go

Install the latest available CLI version with a supported Go toolchain:

```sh
go install github.com/taua-almeida/cs2-analyser-tool@latest
```

`@latest` resolves a pseudo-version of `main` until the first tag is published; after that it selects the latest tagged release.

## Usage

### Analyse

This command parses a CS2 demo file and provides statistical analysis of players' performance.

#### Command Syntax

```bash
analyse [flags]
```

#### Flags

- `-d, --demo <path>`: Path to a CS2 demo file. Repeat the flag in played order to analyse a completed BO3/BO5 series together with `--best-of`.
- `--best-of <n>`: Series format for multiple demos, `3` or `5`. A completed BO3 takes 2 or 3 demos, a completed BO5 takes 3, 4 or 5. Multiple demos always require this flag — the series format is never inferred from the file count — and one demo with it is invalid.
- `-p, --players <players>`: A list of players to analyse. This should be provided as a comma-separated list of player names. Names match case-insensitively, and if any requested name is not in the demo the command fails and lists the available players. In a series, names match any alias the player used across the maps, and a name matching more than one SteamID fails with the candidate SteamIDs.
- `-s, --save`: Flag to save the demo player's data.
- `--save-type <type>`: Type of file to save the data. Options are `json` and `csv`. The default is `json`.
- `--details`: Print the extra stat tables that do not fit the main one: the rating breakdown, approximate Rating 3.0 metrics, multi-kill rounds, trades, CT/T side splits, utility effectiveness and grenades thrown. For a series these show the aggregate values.

#### Options

- `d`: If -d not provided, the CLI will pop a window to select the file in our system. Series analysis never opens the picker: every map must be passed explicitly.
- `p`: If no players are provided, a multiselect option will show on terminal. Series analysis skips the multiselect and analyses everyone unless `--players` is given.

#### Examples

**Analyzing a Specific Demo**

```bash
analyse --demo path/to/demo/file
```
This command will process the specified demo file and output the analysis to the console and will display all available players to analyse.

**Analyzing Specific Players and Saving the Data**

```bash
analyse --demo path/to/demo --players "player1,player2" --save --save-type csv
```

This will analyse only "player1" and "player2" from the specified demo and save the data in CSV format.

**Analyzing a Completed BO3/BO5 Series**

```bash
analyse --best-of 3 --demo map1.dem --demo map2.dem
analyse --best-of 3 --demo map1.dem --demo map2.dem --demo map3.dem
analyse --best-of 5 --demo map1.dem --demo map2.dem --demo map3.dem
```

The demos must be the series' played maps in order. The tool hashes each file (rejecting the same demo supplied twice), parses every map, resolves the two series teams by their rosters, and prints the ordered map results, the overall map and round score, the series winner and an aggregate player table. Only completed competitive 5v5 series are accepted: Wingman-sized maps are rejected, and the final demo must be the map on which a team clinched, so a 1-1 BO3 or a map supplied after the clinch is an error. With `--save` the JSON contains the full series envelope — see [PLAYER_DATA](./_docs/PLAYER_DATA.MD#series-analysis-bo3bo5) — while CSV stays the flat aggregate-player table with the exact single-map columns; full per-map series data requires JSON.

#### Saved files

Saved JSON is a complete analysis record, not a bare player map:

```json
{
  "players": {
    "76561198000000000": { "team_id": 1, "...": "player data" }
  },
  "teams": [
    {
      "team_id": 1,
      "name": "Rooster",
      "aliases": ["Rooster"],
      "score": 13,
      "roster": [76561198000000000]
    },
    { "team_id": 2, "...": "the other logical team" }
  ],
  "map_data": {
    "map_name": "de_mirage",
    "total_rounds": 24,
    "rounds_won_ct": 11,
    "rounds_won_t": 13
  },
  "game_mode": "premier"
}
```

Read players from `.players`, keyed by SteamID (a JSON string). `map_data`, `teams` and `game_mode` describe the whole match and always accompany the players; `--players` limits only `.players`. `game_mode` can be `""` when the demo metadata does not expose it, and `rounds_won_ct`/`rounds_won_t` are the final side scores, not team identities. `teams` carries the two logical teams of the map — the lineups that persist through halftime and overtime side switches — with each team's map-local ID, clan-name aliases, final round wins and SteamID roster; each player references their team through `team_id`. The details, including how identity is resolved and when parsing fails instead of guessing, are in [PLAYER_DATA](./_docs/PLAYER_DATA.MD#logical-teams-teams). CSV remains a flat player-only table with the same columns as before. This is an intentional breaking change from the previous format, which was the bare `{ "<steam-id>": ... }` map now nested under `players`.

#### Analyzed data

The data output showed in the terminal table is not all the analyzed data, to get more info about the available data, go to [PLAYER_DATA](./_docs/PLAYER_DATA.MD). The `Rating` column is an HLTV Rating 3.0-style approximation; how it is calculated, constant by constant, is documented in [RATING](./_docs/RATING.MD).

### Match history

Every successful analysis is also stored automatically in a local SQLite history — single maps and each played map of a `--best-of` series alike. No flag is needed; `--save` remains the separate, explicit file export it always was. If storing fails, the analysis you just saw is still valid: the command prints it, then reports the storage error.

#### Where the history lives, and what it contains

The database is `history.db` inside:

- `$CS2_ANALYSER_HISTORY_DIR` when that environment variable is set, otherwise
- `<user config dir>/cs2-analyser-tool/history` (on Linux `~/.config/cs2-analyser-tool/history`, on macOS `~/Library/Application Support/cs2-analyser-tool/history`, on Windows `%AppData%\cs2-analyser-tool\history`).

Everything is local and private: nothing is ever uploaded, the directory and database are created owner-only where the platform supports it, and the history never stores the original demo bytes nor any demo file path. A match is identified by the SHA-256 of the exact demo bytes that were parsed, so re-analysing the same demo — from any path or filename — never creates a duplicate: the canonical record is kept unchanged and only your player display selection is updated. The stored record is always the complete, unfiltered analysis in the same JSON envelope `--save json` writes; `--players` only chooses who is displayed.

Timestamps in the history are **analysis times** — when you ran the analysis, stored in UTC and displayed in your local time zone. Demos carry no trustworthy record of when the match was played, so no match time is invented.

The schema is versioned with SQLite's `user_version` and every history database is stamped with the tool's `application_id`. A newer database (created by a newer release) is refused with a clear error instead of being downgraded, and a corrupt file, a database another application put at that path, a stranded WAL or journal beside a missing or empty database file, or any file the tool did not create is refused — inspected on a private temporary copy first, so the refused file, its journal and its directory keep their exact bytes: never modified, deleted, recreated or converted.

#### history

```bash
cs2-analyser history
```

Lists stored matches, newest analysis first: a stable 12-character ID (the digest prefix), the local analysis time, map, score and game mode. Logical-team scores are preferred; maps without two resolved teams fall back to the final CT/T side scores. An empty history prints a message and exits successfully.

#### history show

```bash
cs2-analyser history show <id>
cs2-analyser history show <id> --details
```

Re-renders a stored match from the database alone — the original demo is not needed and is never opened. `<id>` is the full SHA-256 or any unique prefix of at least 8 hexadecimal characters; an ambiguous prefix fails listing the candidate IDs. The player selection stored with the match narrows the view exactly as the live rendering did, and the stored record itself always keeps everyone: without a stored selection everyone is shown, and a series map where none of the selected players appeared keeps its explicit empty view rather than falling back to everyone. `--details` adds the same extra stat tables `analyse --details` prints.

#### compare

```bash
cs2-analyser compare --player <steam-id-or-name>
```

Shows a player's Premier trend across the stored history: one row per stored map that is explicitly recorded as `premier` game mode and contains that player, chronologically by analysis time, plus an aggregate totals row. Other modes are never inferred into the trend, and a series is stored only as its individual maps, so nothing is ever counted twice.

`--player` is resolved as a nonzero decimal SteamID64 first; anything else must match exactly one alias observed in the stored matches, compared case-insensitively under Unicode simple case folding: one-to-one case pairs match (`Ö` finds `ö`, Cyrillic `И` finds `и`), while multi-character expansions do not (`STRASSE` does not find `Straße`). Aliases accumulate across matches — a player who renamed still resolves under either name — and an alias shared by several SteamIDs fails with each candidate's SteamID and known aliases so you can pass the SteamID64 instead.

Aggregate values are calculated additively from exact stored facts, never by averaging the per-map numbers you see displayed:

- K/D = sum(kills) / sum(deaths) — a deathless run divides by one
- ADR = sum(damage given) / sum(map rounds)
- KAST = 100 × sum(KAST-credited rounds) / sum(map rounds)
- HS% = 100 × sum(headshots) / sum(kills)
- Opening success = 100 × sum(opening rounds won) / sum(opening kills)

Counts (opening duels, trade kills, deaths traded, utility damage, flashes, grenades thrown) are summed directly. No aggregate rating is fabricated by averaging map ratings.

## Using the engine as a Go library

Everything the CLI computes lives in the importable `analysis` package, which has no CLI, TUI or rendering dependencies of its own:

```go
import "github.com/taua-almeida/cs2-analyser-tool/analysis"
```

Add the module to your project with `go get github.com/taua-almeida/cs2-analyser-tool@latest`.

### Analysing one map

`Analyse` reads a complete demo from any `io.Reader` — a file, an uploaded stream, an in-memory buffer, an object-storage reader — and returns the map's players, logical teams, map data and game mode:

```go
result, err := analysis.Analyse(ctx, reader)
```

`AnalyseFile` is the file convenience wrapper. It owns the file it opens — open failures name the path, and the file is always closed before returning:

```go
result, err := analysis.AnalyseFile(ctx, "match.dem")
```

- **Input ownership**: `Analyse` borrows the reader and never closes it, even when it is an `io.Closer`. No rewindability is assumed and none survives a call: after success, failure or cancellation the reader has been consumed to an unspecified position, so reusing it requires rewinding or reopening it first.
- **Stream failures**: a failing reader is an error, never a panic. An empty stream's immediate `io.EOF` or a connection dropped mid-demo comes back wrapping the reader's own error, so `errors.Is` still sees its identity.
- **Cancellation**: cancelling the context interrupts parsing at the next demo frame instead of being checked only before or after the whole demo, and it also unblocks a `Read` currently blocked inside the reader — a stalled upload or network stream cannot pin the call. The returned error wraps the context's error, so `errors.Is(err, context.Canceled)` and `errors.Is(err, context.DeadlineExceeded)` hold as appropriate. A `Read` abandoned by cancellation may still be running inside the reader when `Analyse` returns; its result is discarded, and the reader stays yours to close if that read itself needs releasing.
- **Team identity**: each map reports two logical teams — the lineups that persist through side swaps — whose IDs are map-local (team 1 played CT in the seeding round). A series numbers its own teams 1 and 2 series-locally, and each series map carries the explicit map-local-to-series translation in `team_assignments`. The two scopes never mix implicitly.

### Building a series

`BuildSeries` aggregates a completed, explicitly-stated BO3 or BO5 from maps parsed with `Analyse`/`AnalyseFile`, supplied in played order with each demo's SHA-256 content digest:

```go
series, err := analysis.BuildSeries(3, []analysis.SeriesMapInput{
    {Demo: mapOne, SHA256: digestOne},
    {Demo: mapTwo, SHA256: digestTwo},
})
```

It is pure aggregation — no I/O, inputs never mutated — and enforces the completed-series rule: the final supplied map must be the clinching one. When the two series teams cannot be resolved from rosters alone, the structured `SeriesTeamConflictError` and `SeriesTeamAmbiguityError` carry the competing evidence and match through `errors.As`.

### JSON contract and v0.x API stability

Every result type marshals with `encoding/json` into the same envelopes the CLI's `--save` writes, documented in [PLAYER_DATA](./_docs/PLAYER_DATA.MD): `MapAnalysis` is the standalone map document, `SeriesAnalysis` the series document embedding each map's unchanged analysis. A series player whose rating cannot be recomputed from raw per-map facts marshals it as `null` rather than a fabricated value.

While releases remain `v0.x` the Go API is not frozen — exported names and fields may still change between minor versions as the surface settles. The JSON contracts are the stable part. The full API reference is available with `go doc github.com/taua-almeida/cs2-analyser-tool/analysis`.

## Contributing

The opt-in external-oracle test for HLTV match 129241 is documented in
[HLTV_REGRESSION.md](./_docs/HLTV_REGRESSION.md). It is separate from the
repository's self-golden integration fixtures and never downloads demos or
scrapes HLTV during a test run.

### CI and test coverage

Pull-request CI restores the two public golden demos, keeps `go vet ./...` and
`go build ./...` as separate checks, and runs uncached tests with
`go test -count=1 -json ./...`. Its Actions summary gives pass, fail, and skip
counts plus every package-relative skipped test. An unlisted skip fails the
workflow, so adding `t.Skip` requires an intentional allowlist review.

The optional External regression workflow is manually dispatched and separate
from pull requests and releases. When its owner-approved private archive is
configured, it runs the eight checksum-pinned HLTV map regressions and the
original BO3 series; absent provisioning configuration is a hard failure. It is
not a release prerequisite. The complete coverage matrix, private archive
contract, summary fields, and manual diagnostic commands are in
[HLTV_REGRESSION.md](./_docs/HLTV_REGRESSION.md#ci-coverage).

For a local run of the ordinary suite:

```sh
make download-test-demos
REQUIRE_TEST_DEMO=1 go test -count=1 ./...
```

### Publishing a release

Releases are maintainer-triggered and remain a human-approved operation:

1. Confirm the normal CI workflow passed on the commit to release.
2. Create an annotated, v-prefixed semantic-version tag, for example `git tag -a v0.1.0 -m "v0.1.0"`.
3. Push only that tag, for example `git push origin v0.1.0`.
4. Watch the release workflow until its verification and publishing jobs finish.
5. In the GitHub release, verify all four platform archives, `checksums.txt`, each archive's contents, and the binary's `version` output.
6. Never move a published tag. Publish a new patch release to correct a release.

### Clone the repo

```bash
git clone https://github.com/taua-almeida/cs2-analyser-tool.git
cd cs2-analyser-tool
```

### Build the project

```bash
go build
```

### Submit a pull request

If you'd like to contribute, please fork the repository and open a pull request to the `main` branch.
