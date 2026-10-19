# install.sh: the release installer

`install.sh` at the root of this repository installs enma from its GitHub Releases. It is
written so that other tools released the same way can use it unchanged except for two lines.
This page is for maintainers of enma and of those tools; users should read the README.

## Reusing it for another tool

Copy `install.sh` and change the two lines of the project configuration:

```sh
APP_NAME="ark"
GITHUB_URL="https://github.com/magicdrive/ark"
```

Outside comments, nothing else in the script names enma, and the tests check that
(`TestConfiguration`). `GITHUB_URL` must be the repository's current name: after a rename or a
transfer, `releases/latest` first redirects to the new name, which the script refuses. The
tests also run the whole install / upgrade / downgrade cycle with the configurations of enma,
[ark](https://github.com/magicdrive/ark) and [goreg](https://github.com/magicdrive/goreg)
(`TestLifecycle`), and `TestRealReleases` (`ENMA_INSTALLER_NETWORK=1`) installs their real
releases from github.com.

The two values are checked before anything else runs: `APP_NAME` is a file name of letters,
digits, `.`, `_` and `-` (not starting with `.` or `-`); `GITHUB_URL` is exactly
`https://github.com/OWNER/REPOSITORY`, with no trailing slash, `.git`, query, fragment, port or
user name.

## The distribution contract

The script works with releases that GoReleaser makes with its defaults, as enma, ark and goreg
do. With `APP` = `APP_NAME`, `TAG` the release tag and `VERSION` = `TAG` without the leading `v`:

| What | Required form |
|---|---|
| Tag | `vMAJOR.MINOR.PATCH`, optionally `-PRERELEASE` (semantic versioning, no `+build`) |
| Latest | `GITHUB_URL/releases/latest` redirects to `GITHUB_URL/releases/tag/TAG` (GitHub does this; drafts and pre-releases are never "latest") |
| Archive | `APP_VERSION_OS_ARCH.tar.gz`, OS `darwin` or `linux`, ARCH `amd64` or `arm64` (GoReleaser's default `name_template`) |
| Checksums | `APP_checksums.txt` in the same release (`checksum.name_template: "{{ .ProjectName }}_checksums.txt"`); lines `<64 lower-case hex digits><two spaces><file name>` (sha256) |
| Older checksums | `APP_VERSION_checksums.txt` (GoReleaser's old default), used only when `APP_checksums.txt` answers 404 |
| Archive content | the binary `APP` at the top (no directory around it); other files are allowed and ignored |
| Version output | the first line of `APP --version` is `APP version vVERSION` or `APP version VERSION` |

So `APP_NAME` must be GoReleaser's `project_name` and the name of the binary. A release that
does not follow this is refused with an error; the script never guesses a file by a partial
name. Windows archives, `386` builds and the bare binaries some old releases have are not used.

When a tool releases differently, change its GoReleaser configuration to this contract rather
than the script: a project-specific branch in the script is not accepted.

## What the script does, in order

1. Checks the configuration and the arguments, finds the tools (`curl`, else GNU Wget; a
   SHA-256 tool), and the platform (`uname -s`, `uname -m`; under Rosetta 2 the arm64 build).
   An unsupported system stops here, before any download.
2. Checks an existing install directory and the file it would replace (see "Where it installs").
3. Resolves the tag: the one given, or the target of the `releases/latest` redirect, which must
   be exactly `GITHUB_URL/releases/tag/<a valid tag>` (case of the owner and repository aside).
   It does not fall back to anything if that fails. It does not use the GitHub API (rate limit,
   JSON).
4. In a new `mktemp -d` directory: downloads the checksum file, finds the archive's line (exactly
   one, by the exact file name; a malformed line rejects the whole file), downloads the archive
   and compares its SHA-256.
5. Decompresses it, lists it with `tar -t` and `tar -tv`, and refuses absolute names, `..`,
   control characters, backslashes, duplicates, entries below `APP/`, anything that is not a
   regular file or a directory, and any warning tar prints while listing. Then it extracts only
   the member `APP` into an empty directory and checks that this directory now holds exactly one
   regular file.
6. Creates the install directory if needed, checks it again, and, if the installed file is
   already identical, stops ("nothing changed").
7. Copies the binary to a temporary file in the install directory, makes it `0755`, checks the
   copy's SHA-256 and that it runs and prints the expected version, then renames it over
   `APP` (`mv` in one directory is `rename(2)`). The temporary directory and file are removed on
   exit, also after `INT`, `TERM` or `HUP`.

## Security model

- Every request starts at `https://github.com/OWNER/REPOSITORY/...`; redirects are followed to
  any host, but only over HTTPS (GitHub sends release files to
  `release-assets.githubusercontent.com`).
- curl (7.20.2 or later, for `--proto-redir`) runs with `-q` (no `.curlrc`),
  `--proto =https --proto-redir =https`, at most 5 redirects, and fails on a connection or a
  transfer that makes no progress for `INSTALLER_TIMEOUT` seconds (default 60); there is no limit
  on the total time of a download that keeps moving.
- GNU Wget (1.17 or later, for `--https-only`; not wget2, whose output differs) runs with
  `--https-only --check-certificate --tries=1 --max-redirect=0`, and the script follows each
  redirect itself after checking that it goes to `https://` (wget alone follows a redirect from
  https to http even with `--https-only`). BusyBox wget is refused: it does not verify
  certificates.
- There is no way to point the script at another server. The tests use the standard
  `https_proxy` and CA variables instead (see "Tests").
- The SHA-256 check finds a broken, truncated or altered download. It does not authenticate the
  release: the checksum file comes from the same release, so whoever can replace the release
  (the repository's owners, a stolen token) can replace both. Signing the checksums (cosign) or
  GitHub artifact attestations would close that; they are not used yet.
- The archive is not trusted even when its checksum matches. Only one member is extracted, into
  an empty private directory, and checked afterwards. tars differ: BusyBox tar rewrites unsafe
  names while listing them (it drops a leading `/` or `../`) and shows a hard link as a regular
  file with ` -> `; both are refused (its warning on stderr; ` -> `). Names with an empty or `.`
  part (`./APP`, `a//b`, `a/./b`) are refused too: tars disagree on whether `./APP` is `APP`, so
  such a pair would install different files on different systems.
- The binary is replaced only by a rename of a complete, checked copy in the same directory, so
  `APP` is at every moment either the old or the new file. Concurrent runs each rename a complete
  file: the last one wins. A run killed with `SIGKILL` (or by a signal in the instant between
  creating its temporary file and recording its name) can leave a `.APP.install.XXXXXX` file in
  the install directory (never a broken `APP`); delete it.
- The script runs the installed `APP --version` to say what it replaces (only if it is a regular,
  executable file of the user's own in the chosen directory; there is no time limit), and the new
  binary from its final directory before the rename. A file that does not report an `APP`
  version is replaced with a warning.
- The README downloads `install.sh` from the `main` branch. For a fixed script, download it from
  a tag instead (`https://raw.githubusercontent.com/OWNER/REPOSITORY/vX.Y.Z/install.sh`) once a
  release that has it exists.

## Where it installs

- Default `$HOME/.local/bin`; `--dir DIR` (relative to the current directory, spaces allowed,
  created with `mkdir -p` if missing). Symbolic links in `DIR` are resolved, and the checks below
  apply to the real directory.
- Refused: a directory writable by every user (`/tmp`), one that is not writable (there is no
  `sudo`), one inside a Homebrew prefix (`brew --prefix`, `$HOMEBREW_PREFIX`, `/opt/homebrew`,
  `/home/linuxbrew/.linuxbrew`, and `/usr/local` when `/usr/local/Homebrew` or
  `/usr/local/Cellar` exists) or inside a `Cellar` directory, a `DIR/APP` that is a symbolic link
  (Homebrew's links, or a link into another directory) or not a regular file. The directory above `DIR`
  must not be writable by every user unless it has the sticky bit (as `/tmp` does). A `DIR` that
  does not exist yet is checked before anything is downloaded or created: it must not contain
  `..` or a `Cellar` part, and the nearest existing directory in its path (by its spelling) must
  pass the same checks. A directory writable by its group is accepted: members of that
  group are trusted.
- `PATH` and shell start-up files are never changed; the script prints the line to add when the
  directory is not in `PATH`, and warns when another `APP` comes first in `PATH`.

## Tests

`internal/installer` (Go, `go test ./internal/installer/`; not on Windows) runs the real
`install.sh` against a fake github.com: a local TLS server with a test CA, reached through a
local HTTPS proxy (`https_proxy`), with `CURL_CA_BUNDLE`, `SSL_CERT_FILE` and `WGETRC` naming the
test CA. Each run has its own `HOME`, `TMPDIR` and a `PATH` of only the tools the script needs, so
the tests can take a tool away or replace it (a failing `mv`, a fake `uname`). The releases are
built by the tests, in the layouts of enma, ark and goreg. No network is needed.

What they cover: install, reinstall, upgrade, downgrade and pre-releases; every command-line
error; the configuration checks; the platforms; wrong `releases/latest` answers; HTTP errors,
truncated and stalled downloads, a refused connection and an untrusted certificate; the
checksum file; unsafe archives; the install directory, Homebrew and links; failures of
`mktemp`, `cat`, `chmod` and `mv`; concurrent and interrupted runs. Each test checks the files
afterwards (the installed binary, its mode, no temporary file left), not only the exit status.

Variables: `ENMA_INSTALLER_SHELLS` (e.g. `sh dash bash ksh busybox`) and
`ENMA_INSTALLER_DOWNLOADERS` (`curl wget`) name the shells and downloaders that must be tested
(`bash` runs with `--posix`, `bash-nonposix` without);
`ENMA_INSTALLER_BUSYBOX` is a busybox to run the BusyBox userland tests with, or `required`;
`ENMA_INSTALLER_NETWORK=1` runs `TestRealReleases`. CI sets them (`ci.yml`; the real releases in
`ci-extended.yml`).
