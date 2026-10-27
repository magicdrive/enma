# Contributing to enma

enma aims to stay a small, robust tool. Bug fixes, tests and documentation improvements are
very welcome; for a new feature or a change of behaviour, please open an issue first so we
can agree on it before you write the code.

## Build and test

Requirements: Go (the version in `go.mod`), a POSIX `sh`, and Linux or macOS for the full
test suite (many tests start real processes).

```bash
go build -o enma .        # the binary is built from the repository root
go vet ./...
go test -count=1 ./...        # unit tests and the CLI end-to-end tests (internal/e2e)
go test -count=1 -race ./...
```

`-count=1` skips cached results: the end-to-end tests build enma with `go build`, so a change
of enma alone would not invalidate their cached result.

`install.sh` is tested by `internal/installer` against a local fake of github.com (no network);
see [docs/installer.md](docs/installer.md) for what it covers and the variables CI sets. Check
the script itself with `sh -n install.sh` and `shellcheck -s sh install.sh`.

CI runs these on Linux and macOS for every pull request with the Go version in `go.mod`, and
`go test -race` with the newest Go release, plus `gofmt`, `staticcheck`, a Windows `go vet`,
`go mod tidy`, `govulncheck`, `actionlint`, the Renovate config validator, a
[Hephaestus](https://github.com/magicdrive/hephaestus) snapshot build of every release archive
checked by `scripts/verify-dist.sh` and installed with `install.sh` (`TestDistArchives`), and
the Homebrew formula installed with the real Homebrew on Linux and macOS
(`scripts/homebrew-e2e.sh`). `make check` runs the local quality gates, `make snapshot` the
release build (nothing is published); releases: [docs/development/releasing.md](docs/development/releasing.md).
The extended checks (`ci-extended.yml`: shuffled tests with several seeds, the process-stopping
tests repeated, short fuzzing, Linux arm64, and the tests and a smoke run on Windows) run weekly,
by hand, and on a pull request labeled `ci-extended`. Tag a release only on a commit whose CI is
green.

Dependencies and GitHub Actions (the Hephaestus action included) are updated by Renovate, and
so are the tool versions in the Makefile marked with a `# renovate:` comment
(`HEPHAESTUS_VERSION`, `GOVULNCHECK_VERSION`, `ACTIONLINT_VERSION`). The Renovate of
`make renovate-check` is pinned, with its whole dependency tree, by
`.github/renovate-check/package-lock.json` and installed with `npm ci` (Node.js of
`.github/renovate-check/.node-version`); Renovate updates it and refreshes the lockfile weekly.
`make renovate-check-test` tests that install against a fake registry.
`STATICCHECK` is updated by hand. Run `make staticcheck` (Go 1.26 or later; CI uses the newest Go) to use the same
staticcheck as CI.

## Pull requests

- Keep a change focused; one topic per pull request.
- Add a test that fails without your change (a regression test for a bug fix).
- Run `gofmt` and the commands above before pushing.
- When you change a flag or a message, update the help texts in `internal/textbank/`,
  the completions in `misc/completions/` (bash, zsh, fish) and the README; tests check that they
  agree. The completion tests (`internal/commandline/completion_test.go`) run each script in its
  shell: install zsh and fish to run them all (CI does). After changing the bash or zsh script,
  rebuild the combined one: `go test ./internal/commandline -run TestCombinedCompletionScript -update`.
- Describe a user-visible change in the pull request: the release notes on
  [GitHub Releases](https://github.com/magicdrive/enma/releases) are the record of what changed, and are written from it.

## Reporting bugs

Please include the enma version (`enma --version`), your OS, the command line or config file
(without secrets), what you expected and what happened. Security issues: see
[SECURITY.md](SECURITY.md).
