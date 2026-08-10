# Integration test fixtures

The golden files here are checked in; the `.dem` demos they were generated
from are not, because they are tens of megabytes. Fetch them with:

```sh
make download-test-demos
```

Both demos are pinned by SHA-256 in the `Makefile` and in
`integration_test.go`, so the bytes cannot change underneath the goldens.
Without them `go test ./...` skips the integration test.

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

## Regenerating the goldens

Only when a behaviour change is intentional — a diff here is a stat
regression until proven otherwise:

```sh
go test ./cmd/demo_parser -run TestProcessDemoGolden -update
```
