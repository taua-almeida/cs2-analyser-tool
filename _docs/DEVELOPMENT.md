# Development and releases

## Tests and CI

The opt-in external-oracle test for HLTV match 129241 is documented in [HLTV regression](./HLTV_REGRESSION.md). It is separate from the repository's self-golden integration fixtures and never downloads demos or scrapes HLTV during a test run.

Pull-request CI checks canonical Go formatting, verifies that the application
and tool module files are tidy, and runs the pinned Staticcheck version before
restoring the two public golden demos. It keeps `go vet ./...` and
`go build ./...` as separate checks, and runs uncached tests with
`go test -count=1 -json ./...`. Its Actions summary gives pass, fail, and skip
counts plus every package-relative skipped test. An unlisted skip fails the
workflow, so adding `t.Skip` requires an intentional allowlist review.

The optional External regression workflow is manually dispatched and separate from pull requests and releases. When its owner-approved private archive is configured, it runs the eight checksum-pinned HLTV map regressions and the original BO3 series; absent provisioning configuration is a hard failure. It is not a release prerequisite. The complete coverage matrix, private archive contract, summary fields, and manual diagnostic commands are in [HLTV regression](./HLTV_REGRESSION.md#ci-coverage).

For a local run of the ordinary suite:

```sh
make download-test-demos
REQUIRE_TEST_DEMO=1 go test -count=1 ./...
```

## Publishing a release

Releases are maintainer-triggered and remain a human-approved operation:

1. Confirm the normal CI workflow passed on the commit to release.
2. Create an annotated, v-prefixed semantic-version tag, for example `git tag -a v0.1.0 -m "v0.1.0"`.
3. Push only that tag, for example `git push origin v0.1.0`.
4. Watch the release workflow until its verification and publishing jobs finish.
5. In the GitHub release, verify all four platform archives, `checksums.txt`, each archive's contents, and the binary's `version` output.
6. Never move a published tag. Publish a new patch release to correct a release.

## Clone and build

```sh
git clone https://github.com/taua-almeida/cs2-analyser-tool.git
cd cs2-analyser-tool
go build
```

Contributing guidance, build and test instructions, and pull-request
requirements live in [CONTRIBUTING.md](../CONTRIBUTING.md).
