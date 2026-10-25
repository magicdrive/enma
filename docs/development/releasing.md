# Releasing enma

enma is released with [Hephaestus](https://github.com/magicdrive/hephaestus) v1.2.0
(`hephaestus.yaml`; the action and `HEPHAESTUS_VERSION` in the `Makefile` are pinned to the
same release). A release is published only by the release workflow
(`.github/workflows/release.yml`) when a `v*` tag is pushed; nothing is published from a
workstation. `make publish vX.Y.Z` ([`hephaestus publish`](https://github.com/magicdrive/hephaestus/blob/main/docs/publish.md))
checks the release, pushes the tag, follows the release run and verifies what it published.

## What a release produces

The same files as the releases made with GoReleaser up to v2.1.0, which `install.sh`
(docs/installer.md, "The distribution contract") and the Homebrew formula depend on:

| File | Content |
| --- | --- |
| `enma_<version>_{darwin_amd64,darwin_arm64,linux_386,linux_amd64,linux_arm64}.tar.gz` | `enma` at the top, `LICENSE`, `README.md`, `completions/` |
| `enma_<version>_{windows_386,windows_amd64,windows_arm64}.zip` | `enma.exe` at the top, the same other files |
| `enma_checksums.txt` | `<sha256>  <file>` for every archive |

`enma --version` prints `enma version v<version>`. The binaries are built with the Go toolchain
pinned in `hephaestus.yaml` (`builds[].env.GOTOOLCHAIN`, updated by Renovate);
`scripts/release-toolchain.sh` fails the build when a binary was built with another Go, or when
the pin is older than go1.27.2 (v2.1.0). `go.mod` keeps only the oldest supported Go. The formula `enma.rb` in
[magicdrive/homebrew-tap](https://github.com/magicdrive/homebrew-tap) installs the binary and
the bash, zsh and fish completions. Build provenance attestations are created for the archives
and the checksum file (`gh attestation verify <file> -R magicdrive/enma`).

## Prerequisites (repository settings)

- Environment `release`, deployment restricted to tags `v*`, holding the secret
  `HOMEBREW_TAP_GITHUB_TOKEN`: a fine-grained token (or GitHub App token) with
  `contents: write` and `pull-requests: write` on `magicdrive/homebrew-tap` only.
- The release itself is published with the workflow's `GITHUB_TOKEN`; no personal token is
  used (`PERSONAL_GITHUB_TOKEN` is no longer needed by the release).
- An active branch ruleset on `main` with the required status check `ci-gate` (the job of
  `ci.yml` that needs every other job), and an active tag ruleset on `v*` with the rules
  deletion and update. `make publish` checks both and refuses to tag without them.
- The environment `release` is deployable from tags only (a `v*` tag policy, no branch
  policy).
- These are the [conventions](https://github.com/magicdrive/hephaestus/blob/main/docs/publish.md#conventions)
  of `hephaestus publish`: `make publish-check vX.Y.Z` lists every one that is not met.

## Steps

1. Make sure the change log of the release is clear from the merged pull requests: GitHub
   generates the release notes from them when the release is created
   (`release.github.generate_notes`); edit them on the release page afterwards if needed.
2. On an up-to-date `main` whose CI (`ci-gate`) is green for the push of the commit to tag,
   check the release (needs `git`, an authenticated `gh` and GNU Make; run from the
   repository root):

   ```sh
   make publish-check vX.Y.Z      # the publish preflight: git, CI, settings, tap; writes nothing
   make snapshot                  # optional: build every archive locally, nothing is published
   ```

3. Release: `make publish vX.Y.Z` (also `VERSION=vX.Y.Z`, or `tag=vX.Y.Z` as before). It runs
   the preflight again (including `hephaestus preflight --strict`), asks for confirmation
   (default No; `YES=1` skips it), creates and pushes one annotated tag, follows the release
   workflow run, and verifies the release: assets, checksums, attestations, and the tap pull
   request (or, for a prerelease, that the tap was not touched). The tag push is the only
   write and cannot be taken back.
   `make publish-status vX.Y.Z` shows what exists for a version (read-only),
   `make publish-watch vX.Y.Z` follows an already pushed tag again, and `make publish-help`
   explains the rules.
4. The release workflow runs:
   - `ci`: the whole CI (`ci.yml`) at the tagged commit;
   - `build`: builds, archives and checksums with Hephaestus with a read-only token and no
     secret, verifies the archives (`scripts/verify-dist.sh`) and the Go toolchain of the
     binaries (`scripts/release-toolchain.sh`), installs them with `install.sh`
     (`TestDistArchives`) and uploads `dist/`;
   - `release` (environment `release`): plans the release (`command: plan`: prerelease,
     tap publish/skip), rehearses the tap pull request (read-only), attests every archive
     and the checksum file, publishes the GitHub Release, and opens the formula pull request
     on `magicdrive/homebrew-tap` (branch `enma-X.Y.Z`). The two tap steps run, and receive
     the tap token, only when the plan says `tap=publish`.
5. Review and merge the pull request on `magicdrive/homebrew-tap`. Until it is merged,
   `brew upgrade enma` still installs the previous version.

A pre-release tag (`vX.Y.Z-rc.1`) is published as a pre-release and does not update the tap
(`brews[].skip_upload: auto`): the plan says `tap=skip`, so the tap steps are skipped and the
tap token is not even passed to them.

Before Hephaestus v1.2.0, `make publish tag=vX.Y.Z` ran the tests and `hephaestus preflight`,
then created a lightweight tag and pushed it, and stopped there. It now runs
`hephaestus publish`: no local test run (the preflight requires the green CI of the commit
instead), a confirmation, an annotated tag, and the run followed and verified to the end.

## Recovery

- **`make publish` was interrupted** (Ctrl-C, network, timeout): only the monitoring stopped
  (exit code 3, outcome unknown). Run `make publish-status vX.Y.Z`, then
  `make publish-watch vX.Y.Z` if the tag is on `origin`.
- **The workflow failed before the `release` job**: nothing was published. A transient
  failure: re-run the failed jobs. Otherwise fix the cause on `main` and release a new
  version (for example the next `-rc.N`); the tag ruleset forbids moving or deleting a tag.
- **The release job failed after the release was published** (for example at the tap step):
  re-run the failed jobs. Hephaestus continues from the release: identical assets are left
  alone, and an existing tap pull request for the same version is reused.
- **A published release is wrong**: assets of a published release are never replaced; release
  a new patch version.
- Hephaestus's own guide: <https://github.com/magicdrive/hephaestus/blob/main/docs/recovery.md>.

## Rolling back to GoReleaser

The move to Hephaestus is one pull request; reverting it restores `.goreleaser.yml` and the
GoReleaser release workflow (which publishes with `PERSONAL_GITHUB_TOKEN` and pushes the
formula directly to the tap's `main`).
