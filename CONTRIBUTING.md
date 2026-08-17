# Contributing to cs2-analyser-tool

Keep changes focused and include the tests and documentation needed to explain
them. Pull requests target the `main` branch.

Participation in this project is governed by the
[`CODE_OF_CONDUCT.md`](./CODE_OF_CONDUCT.md).

## Protect demo and player data

CS2 demo files, exported JSON or CSV, history databases, command output, and
logs can expose player names, SteamIDs, match data, and local filesystem paths.
Before putting anything in an issue, pull request, or commit:

- Do not attach or commit private `.dem` files, history databases, exports, or
  raw logs.
- Remove personal directory names and other sensitive paths from commands and
  error messages.
- Redact private player data and SteamIDs. Public fixture data may be shared
  only when its source and redistribution terms permit it.
- Remove credentials, tokens, and other secrets.

The repository ignores `*.dem`, but an ignore rule is not a privacy guarantee.
The two public integration demos are downloaded from their documented sources;
private and external regression demos stay outside Git. See
[`analysis/testdata/README.md`](./analysis/testdata/README.md) for their source,
license, checksum, and storage rules.

Report suspected vulnerabilities through the private process in
[`SECURITY.md`](./SECURITY.md), not through a public issue.

## Prerequisite and repository layout

Use Go 1.26 or later, as required by `go.mod` and the installation guide.

- `main.go` and `cmd/` contain the Cobra CLI, terminal UI, and output adapters.
- `analysis/` is the public Go package and contains parser, aggregation, and
  golden regression tests.
- `internal/history/` contains the local SQLite history implementation.
- `internal/citest/` validates CI test results and the repository's explicit
  skip policy.
- `tools/` pins development tools in a separate Go module so their dependencies
  cannot change the application module's build list.
- `_docs/` contains the CLI, public package, data-contract, rating, testing, and
  release documentation.
- `analysis/testdata/` contains checked-in golden JSON and documentation for
  demo fixtures. Demo bytes are not committed.

## Build and test

Clone and build all packages with the same build command used by CI:

```sh
git clone https://github.com/taua-almeida/cs2-analyser-tool.git
cd cs2-analyser-tool
go build ./...
```

For the ordinary local suite, run either command:

```sh
go test ./...
make test
```

Without the public demo bytes, the affected golden integration subtests skip.
Unit and synthetic tests still run. To restore the two checksum-pinned public
demos and require their integration tests, use the documented CI-equivalent
commands:

```sh
make download-test-demos
REQUIRE_TEST_DEMO=1 go test -count=1 ./...
```

`make download-test-demos` uses the network and writes the ignored
`analysis/testdata/mirage.dem` and `analysis/testdata/ancient.dem` files after
verifying their SHA-256 checksums. The manually dispatched External regression
workflow uses a separate owner-approved archive. It is not a pull-request or
release prerequisite, and its private demo files must remain outside Git. See
[`_docs/HLTV_REGRESSION.md`](./_docs/HLTV_REGRESSION.md) for that workflow and
its fixture contract.

## Format and check Go changes

Apply canonical Go formatting, then run the same non-mutating formatting and
static-analysis checks used by pull-request CI:

```sh
make fmt
make fmt-check
make tidy-check
make lint
```

`make fmt-check` uses `go list` to find package source and test files, lists
every file that needs formatting, and fails without editing it.
`make tidy-check` verifies both the application and tool module files without
changing them. `make lint` runs
`go tool -modfile=tools/go.mod staticcheck ./...` using the
`honnef.co/go/tools` version pinned in `tools/go.mod` (Staticcheck 2026.1,
module version `v0.7.0`). The first run may download that pinned tool and its
isolated module dependencies through Go's tool dependency mechanism.

Run all non-mutating checks above plus the ordinary test suite with:

```sh
make check
```

Run the existing vet, build, and uncached test checks separately:

```sh
go vet ./...
go build ./...
go test -count=1 ./...
```

Pull-request CI downloads the public demos, sets `REQUIRE_TEST_DEMO=1`, and
rejects unlisted test skips. A separate pull-request job checks the pinned
GoReleaser configuration and verifies a snapshot release without publishing
it.

## Tests, goldens, and documentation

- Add or update focused tests when behavior changes. Adding `t.Skip` also
  requires an intentional review of the CI skip allowlist.
- Treat a golden JSON diff as a behavior change. When a parser change is
  intentional and the matching demo is available, regenerate the golden with:

  ```sh
  go test ./analysis -run TestAnalyseGolden -update
  ```

  Review every changed field before submitting it.
- The repository has no general generated-file command. State the exact command
  used for any generated output in the pull request, and do not include local
  build or release output from `build/` or `dist/`.
- Update the relevant README or `_docs/` page when changing CLI flags or output,
  exported JSON or CSV, the public `analysis` API, fixture behavior, or release
  artifacts.

## Open a pull request

Fork the repository and open a focused pull request to `main`. Complete the
pull-request template with:

- a summary and any related issue;
- the commands and manual checks you ran, including anything you could not run;
- documentation changes;
- CLI, JSON/CSV, or public Go API compatibility effects; and
- fixture, golden-file, generated-file, and demo-data considerations.

Maintainers publish releases through the process documented in
[`_docs/DEVELOPMENT.md`](./_docs/DEVELOPMENT.md#publishing-a-release).
