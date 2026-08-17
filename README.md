# cs2-analyser-tool

[![CI](https://github.com/taua-almeida/cs2-analyser-tool/actions/workflows/ci.yml/badge.svg)](https://github.com/taua-almeida/cs2-analyser-tool/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/taua-almeida/cs2-analyser-tool)](https://github.com/taua-almeida/cs2-analyser-tool/releases)
[![Go Reference](https://pkg.go.dev/badge/github.com/taua-almeida/cs2-analyser-tool/analysis.svg)](https://pkg.go.dev/github.com/taua-almeida/cs2-analyser-tool/analysis)
[![License](https://img.shields.io/github/license/taua-almeida/cs2-analyser-tool)](./LICENSE)

`cs2-analyser-tool` parses Counter-Strike 2 demo files into readable match and player statistics for players, coaches, and analysts. Use the CLI for interactive tables, JSON or CSV exports, completed-series analysis, and private local match history, or import the same analysis engine as a Go package.

## Features

- Analyse one map or a completed best-of-3/best-of-5 series.
- Inspect kills, assists, ADR, KAST, opening duels, utility, side splits, trades, multi-kills, and an HLTV Rating 3.0-style approximation.
- Choose demos and players through terminal-based pickers, or provide everything as flags for scripts.
- Export complete analyses as JSON or flat player tables as CSV.
- Keep a deduplicated local SQLite history and compare a player's Premier results over time.
- Use the parser and series aggregation directly from the public Go `analysis` package.

Demo files are parsed locally. The history database does not contain demo bytes or demo file paths, and the tool does not upload analysis data.

## Installation

### Prebuilt release

Download the archive and `checksums.txt` for your platform from [GitHub Releases](https://github.com/taua-almeida/cs2-analyser-tool/releases). The [installation guide](./_docs/INSTALLATION.md) lists every archive and includes checksum and extraction commands.

### Go install

With Go 1.26 or later, ensure the Go install directory is on `PATH`. Go uses `GOBIN` when it is set; otherwise it uses the `bin` directory under `GOPATH`.

```sh
go install github.com/taua-almeida/cs2-analyser-tool@latest
cs2-analyser-tool version
```

## Five-minute quick start

Start with a CS2 `.dem` file and run:

```sh
cs2-analyser-tool analyse --demo /path/to/match.dem
```

After parsing, use the arrow keys to move, Space or Enter to select players, and `y` to confirm. Pressing `q` or Ctrl+C closes the player selector without a selection; it does not cancel the command, which continues by printing and storing everyone. The result is printed as a terminal table and stored automatically in the local history.

If you omit `--demo`, the file picker appears inside the current terminal. It starts in your home directory when the operating system can resolve it, otherwise in the current directory. It does not open a desktop window.

Useful next commands:

```sh
# Include the additional rating, trade, side, and utility tables.
cs2-analyser-tool analyse --demo /path/to/match.dem --details

# Save the complete selected analysis as JSON in the current directory.
cs2-analyser-tool analyse --demo /path/to/match.dem --save --save-type json

# List analyses already stored in local history.
cs2-analyser-tool history
```

`--save` names the file `<unix-seconds>_data.json` (or `.csv`) and prints the exact filename after writing it.

Run `cs2-analyser-tool --help` or read the [CLI guide](./_docs/CLI.md) for player selection, CSV export, series analysis, history replay, and Premier trends.

## Representative output

This is the current main-table rendering for two players from the checked-in Mirage golden data. Processing and timing lines are omitted; `--details` prints more stat groups in separate tables.

```text
$ cs2-analyser-tool analyse --demo match.dem --players "123,NIGHTSOUL"
+--------------+-----------+--------+-------+----+---------+-------+----------+--------+-------+---------------+-------------+
| NAME         |     KILLS | DEATHS | K/D   | HS | ASSISTS | ADR   | KAST (%) | RATING | ENTRY | PRECISION (%) | BEST WEAPON |
+--------------+-----------+--------+-------+----+---------+-------+----------+--------+-------+---------------+-------------+
| 123          |        11 |      4 | 2.750 |  8 |       1 | 113.9 | 100.0    | 1.44   | 1:0   | 72.7          | AK-47       |
| NIGHTSOUL    |        10 |      5 | 2.000 |  6 |       1 | 94.2  | 90.0     | 1.17   | 5:0   | 60.0          | AK-47       |
+--------------+-----------+--------+-------+----+---------+-------+----------+--------+-------+---------------+-------------+
| MAP PLAYED   | DE_MIRAGE |        |       |    |         |       |          |        |       |               |             |
| SCORE CT : T |     2 : 8 |        |       |    |         |       |          |        |       |               |             |
+--------------+-----------+--------+-------+----+---------+-------+----------+--------+-------+---------------+-------------+
This is a demo of a: PREMIER game
History: stored match 84a1a4191302
```

## Documentation

- [Installation](./_docs/INSTALLATION.md): release archives, checksum verification, extraction, and Go installation.
- [CLI guide](./_docs/CLI.md): `analyse`, exports, completed series, local history, and `compare`.
- [Player data](./_docs/PLAYER_DATA.MD): JSON and CSV contracts plus field-by-field semantics.
- [Rating methodology](./_docs/RATING.MD): every formula, constant, limitation, and worked example.
- [Go package guide](./_docs/GO_LIBRARY.md) and [generated API reference](https://pkg.go.dev/github.com/taua-almeida/cs2-analyser-tool/analysis).
- [Testing and HLTV regression](./_docs/HLTV_REGRESSION.md): fixture contracts, coverage, and diagnostics.
- [Development, contributing, and releases](./_docs/DEVELOPMENT.md): local build/test commands, pull requests, and the maintainer release checklist.

## Contributing

See [Development, contributing, and releases](./_docs/DEVELOPMENT.md) for the repository setup, test suites, pull-request entry point, and release process.

## License

This project is available under the [MIT License](./LICENSE).
