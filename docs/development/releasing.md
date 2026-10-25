# Releasing enma

enma is released with [Hephaestus](https://github.com/magicdrive/hephaestus) v1.1.0
(`hephaestus.yaml`). A release is published only by the release workflow
(`.github/workflows/release.yml`) when a `v*` tag is pushed; nothing is published from a
workstation.

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
- Recommended (reported as `WARN` by the preflight): a tag ruleset on `v*` that forbids
  deleting and moving tags, and the CI checks required on `main`.

## Steps

1. Make sure the change log of the release is clear from the merged pull requests: GitHub
   generates the release notes from them when the release is created
   (`release.github.generate_notes`); edit them on the release page afterwards if needed.
2. On an up-to-date `main` whose CI is green, check the release (a GitHub token is needed,
   e.g. `GH_TOKEN=$(gh auth token)`):

   ```sh
   make preflight tag=vX.Y.Z      # hephaestus preflight --strict: git, CI, settings, tap
   make snapshot                  # optional: build every archive locally, nothing is published
   ```

3. Push the tag: `make publish tag=vX.Y.Z` runs the tests and the preflight (without
   `--strict`: a `FAIL` or `UNKNOWN` stops it, a `WARN` does not), then creates and pushes the
   tag.
4. The release workflow runs:
   - `ci`: the whole CI (`ci.yml`) at the tagged commit;
   - `build`: builds, archives and checksums with Hephaestus with a read-only token and no
     secret, verifies the archives (`scripts/verify-dist.sh`), installs them with `install.sh`
     (`TestDistArchives`) and uploads `dist/`;
   - `release` (environment `release`): rehearses the tap pull request (dry run), attests the
     archives, publishes the GitHub Release, and opens the formula pull request on
     `magicdrive/homebrew-tap` (branch `enma-X.Y.Z`).
5. Review and merge the pull request on `magicdrive/homebrew-tap`. Until it is merged,
   `brew upgrade enma` still installs the previous version.

A pre-release tag (`vX.Y.Z-rc.1`) is published as a pre-release and does not update the tap
(`brews[].skip_upload: auto`).

## Recovery

- **The workflow failed before the `release` job**: nothing was published. Fix the cause on
  `main`; delete the tag only if it was never published (`git push origin :refs/tags/vX.Y.Z`
  and `git tag -d vX.Y.Z`), then tag again. Otherwise release the next patch version.
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
