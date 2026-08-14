# Integration test fixtures

The golden files here are checked in. Restore the two public demo fixtures with:

```sh
make download-test-demos
```

All demos are pinned by SHA-256 in `integration_test.go`; checksums for the two
downloaded fixtures are repeated in the `Makefile`. Without the demo bytes,
`go test ./...` skips the affected integration subtest. CI sets
`REQUIRE_TEST_DEMO=1`, which makes missing Mirage or Ancient bytes fail instead.
That public-fixture requirement is independent of the private fixture flag.

The `hltv-*` directories contain JSON oracles only. Their audited demos stay
outside Git and are selected with `HLTV_DEMO_DIR` or the OS path-list
`HLTV_EXTRA_DEMO_DIRS`; see [`_docs/HLTV_REGRESSION.md`](../../_docs/HLTV_REGRESSION.md).

## mirage.dem

A CS2 match on de_mirage from January 2024, still carrying `round_mvp` game
events. Used to exercise the `RoundMVPAnnouncement` handler.

- Source: [LaihoE/demoparser](https://github.com/LaihoE/demoparser), file
  `src/parser/test_demo.dem`, pinned to commit `4131a4fc02fda291b22421c20e1ca33f149535a7`
- License: MIT
- SHA-256: `84a1a4191302bdd2a3bbb5a727842093744b1fb1a228aeec630369e44b622cb2`

## ancient.dem

A later Valve Premier match on de_ancient with no `round_mvp` events at all,
where MVP counts can only come from the scoreboard entity property.

- Title: match730_003736456444682174484_1173793269_201
- Creator: Peter Xenopoulos
- Source: <https://figshare.com/articles/media/match730_003736456444682174484_1173793269_201/28440473>
- DOI: [10.6084/m9.figshare.28440473.v1](https://doi.org/10.6084/m9.figshare.28440473.v1)
- License: [CC BY 4.0](https://creativecommons.org/licenses/by/4.0/)
- SHA-256: `b29a9cb537a181deef97b15cfed10ee722a37999644a27bb2226fdd77a1337fc`

## inferno-shotgun.dem

A 20-round Valve Premier match on de_inferno. It contains 29 XM1014
`PlayerHurt` events, including four where the per-event damage cap removes
overlapping pellet damage. It also crosses halftime and contains one player
joining a side between `RoundStart` and the end of freeze time.

- Original Valve replay: `match730_003835545804819398890_1582373632_202`
- Source: optional local fixture; the repository does not distribute the demo
- Setup: place the exact replay at `analysis/testdata/inferno-shotgun.dem`
- Golden output redistribution is approved by the repository owner, including
  the player names and Steam IDs it contains
- Size: 288,711,083 bytes
- SHA-256: `095625b47c2cc6ace12414a6bbc987ea254904d969ae39fb95c7d54e085f7f93`

By default a missing private replay produces an explicit skip. Set
`REQUIRE_PRIVATE_TEST_DEMO=1` when the exact bytes have been provisioned; the
test then fails if the file is missing, and a checksum mismatch always fails.
`REQUIRE_TEST_DEMO=1` never makes this private fixture required and
`REQUIRE_PRIVATE_TEST_DEMO=1` never weakens or replaces the two public fixture
requirements.

The scheduled/manual external workflow can accept this replay only as the
optional `private-golden/inferno-shotgun.dem` member of the owner-approved
archive documented in
[`_docs/HLTV_REGRESSION.md`](../../_docs/HLTV_REGRESSION.md#owner-approved-archive-contract).
It verifies the archive and replay before linking the temp-only file into this
directory for the test process. Private demos are never stored in the GitHub
Actions cache or uploaded as workflow artifacts.

## Regenerating the goldens

Only when a behaviour change is intentional — a diff here is a stat
regression until proven otherwise:

```sh
go test ./analysis -run TestAnalyseGolden -update
```
