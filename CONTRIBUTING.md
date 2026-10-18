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

CI runs these on Linux and macOS for every pull request with the Go version in `go.mod`, and
`go test -race` with the newest Go release, plus `gofmt`, `staticcheck`, a Windows `go vet`,
`govulncheck` and a GoReleaser snapshot build checked by `scripts/verify-dist.sh`.
The extended checks (`ci-extended.yml`: shuffled tests with several seeds, the process-stopping
tests repeated, short fuzzing, Linux arm64, and the tests and a smoke run on Windows) run weekly,
by hand, and on a pull request labeled `ci-extended`. Tag a release only on a commit whose CI is
green.

Dependencies and GitHub Actions are updated by Renovate. The tool versions pinned in the
workflows and the Makefile (`STATICCHECK`, `govulncheck@…`, GoReleaser `version:`) are updated
by hand. Run `make staticcheck` (Go 1.26 or later; CI uses the newest Go) to use the same
staticcheck as CI.

## Pull requests

- Keep a change focused; one topic per pull request.
- Add a test that fails without your change (a regression test for a bug fix).
- Run `gofmt` and the commands above before pushing.
- When you change a flag or a message, update the help texts in `internal/textbank/`,
  the completions in `misc/completions/` and the README; tests check that they agree.
- Add a line to the `Unreleased` section of `CHANGELOG.md` for a user-visible change.

## Reporting bugs

Please include the enma version (`enma --version`), your OS, the command line or config file
(without secrets), what you expected and what happened. Security issues: see
[SECURITY.md](SECURITY.md).
