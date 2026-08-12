# HLTV regression harness

`TestHLTVRegression` checks parser output for HLTV match 129241 against static
HLTV values in `cmd/demo_parser/testdata/hltv-129241/expected.json`. HLTV is an
external correctness oracle for this test. Analysing one CS2 Premier demo at a
time remains the product's primary workflow; this harness does not aggregate
the three maps into a best-of-three result.

The test never contacts HLTV, downloads a demo, or reads an ignored `build/`
artifact. The fixture records the source match/map URLs, reviewed HLTV
nicknames, SteamID64 mappings, and expected values so changes are visible in a
normal code review.

## Demo setup

Set `HLTV_DEMO_DIR` to a directory with this exact layout:

```text
<HLTV_DEMO_DIR>/
├── rooster-vs-mindfreak-m1-inferno.dem
├── rooster-vs-mindfreak-m2-anubis.dem
└── rooster-vs-mindfreak-m3-mirage.dem
```

The files are external and must not be copied into the repository. Before
parsing, the test streams the entire selected file through SHA-256 and requires
the following digest:

| Map | SHA-256 |
| --- | --- |
| Inferno | `60129e983bb529bd77b642d59bd2e172367b6ab0dbe73849bb656f7eb76d43c4` |
| Anubis | `7b2a1c89ea0b99be5d4874452716f25cfcbd49353c49b3402cc107f0c5a4bcae` |
| Mirage | `53a3ab2814af90cb9898f2e8f3d7d14ae254d94f020de418ec307e57abd7008a` |

A checksum mismatch always fails and identifies the affected file, actual
digest, and required digest.

## Commands and missing-demo behavior

Normal test runs need no external data. With `HLTV_DEMO_DIR` unset, the harness
loads and validates its static fixture, then clearly skips:

```sh
go test -count=1 ./...
```

Run every available map with:

```sh
HLTV_DEMO_DIR=/path/to/match-129241-demos \
  go test -count=1 -v ./cmd/demo_parser -run '^TestHLTVRegression$'
```

If the directory is set but a map file is absent, that map subtest skips. Make
the environment variable and every selected demo mandatory with
`REQUIRE_HLTV_DEMOS=1`; this is the full three-map regression command:

```sh
HLTV_DEMO_DIR=/path/to/match-129241-demos \
REQUIRE_HLTV_DEMOS=1 \
  go test -count=1 -v ./cmd/demo_parser -run '^TestHLTVRegression$'
```

With required mode enabled, an unset `HLTV_DEMO_DIR` or absent selected demo is
a test failure. Run one map independently by selecting its table-driven
subtest, for example Inferno:

```sh
HLTV_DEMO_DIR=/path/to/match-129241-demos \
REQUIRE_HLTV_DEMOS=1 \
  go test -count=1 -v ./cmd/demo_parser -run '^TestHLTVRegression$/^inferno$'
```

The other subtest names are `anubis` and `mirage`.

## Comparison contract

Each map must contain exactly the ten fixture SteamID64 values. Display names
are diagnostic labels only; nicknames never establish identity. Map name,
round count, the two exact final score values, kills, and deaths require strict
parity. Score values are sorted before comparison because the parser exposes
final CT/T scoreboard fields rather than stable team identity; the values
themselves are not given a tolerance.

ADR is compared exactly after formatting both sides to the one-decimal value
shown by HLTV and the CLI. HLTV KAST percentages are converted to integer
qualifying-round counts with `round(percent * map rounds / 100)` before
comparison.

The single #39 ADR difference and all #38 KAST differences live together in
`hltvExpectedDifferences`. Every row pins the map, SteamID64, metric, HLTV
value, current tool value, and issue. A third value fails as a regression. If
the parser reaches HLTV parity, the test also fails with a request to remove
the stale exception and promote that row to strict parity. This harness does
not change or relax the behavior owned by #38 or #39.

Rating 3.0 is approximate because HLTV's formula is proprietary. The harness
therefore reports, but never asserts, exact rating parity: mean absolute error,
root mean squared error, bias, Spearman rank correlation, and counts within
±0.05, ±0.10, and ±0.20. The report uses the same two-decimal rating display
values users see in the CLI.
