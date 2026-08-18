# Security policy

## Reporting a vulnerability

Report suspected vulnerabilities through GitHub's private vulnerability
reporting: open the repository's Security tab and use the "Report a
vulnerability" button, or go directly to
https://github.com/taua-almeida/cs2-analyser-tool/security/advisories/new.

Do not open a public issue for a suspected vulnerability. The report is
not public. The reporter and the people collaborating on the advisory can see
it; details become public only if a maintainer publishes the advisory.

## Security context

The CLI and public `analysis` package parse `.dem` files supplied by users.
Treat every demo as untrusted input, even though parsing happens locally and
the tool does not upload analysis data. Report unexpected file access or
writes, crashes, panics, or uncontrolled resource use caused by a crafted or
malformed demo through the private channel above.

## What to include

- The affected command or public Go API and the tool or module version.
- The observed security impact and a minimal reproduction.
- The repository's privacy rules still apply to vulnerability reports. Do not
  attach private `.dem` files, history databases, exports, raw logs, or other
  data containing player names, SteamIDs, or sensitive filesystem paths. If
  reproduction requires demo bytes, describe how the demo was obtained or
  constructed and wait for a maintainer to request a sanitized sample through
  the advisory.
- Whether the issue also affects the latest release or only unreleased
  changes on `main`.

## Dependency and vulnerability monitoring

Dependabot checks the Go modules and GitHub Actions weekly through
`.github/dependabot.yml`. With Dependabot alerts and security updates enabled,
an advisory affecting a dependency produces a security-update pull request
immediately instead of waiting for the weekly schedule. No dependency pull
request is merged automatically; each one passes CI and maintainer review, as
described in [`CONTRIBUTING.md`](./CONTRIBUTING.md).

The repository also runs `govulncheck` at the version pinned in
`tools/go.mod`, locally via `make vulncheck` and in CI on relevant pull
requests, pushes to `main`, a weekly schedule, and manual dispatch. It reports
known vulnerabilities reachable from the compiled packages, scanning every
released platform because reachability differs per GOOS/GOARCH. A reachable
finding is never silently suppressed: it is fixed by a dependency update or
tracked in a public issue, or in a private advisory while details must stay
confidential. Findings are prioritized by reachability and impact on demo
parsing, local data, and released binaries, and fixes are published under the
release policy below.

## Supported versions

Only the latest release is supported. Security fixes land on `main` and are
published in a new release. Older releases do not receive security updates;
upgrade to the latest release to receive fixes.
