# Changelog

All notable changes to enma are listed here. Releases up to v1.2.10 are described on the
[Releases page](https://github.com/magicdrive/enma/releases).

## Unreleased

### Changed (behaviour differs from v1.2.10)

- Processes a build or command leaves behind are stopped when it returns (SIGTERM, then
  SIGKILL after 5s), in every mode. Use `--daemon` (hotload) for a process that should keep
  running. To keep a helper running in watch mode, start it outside enma, or detach it into its
  own session (e.g. `setsid` on Linux) — enma then no longer stops it at all.
- `.enmaignore` files in parent directories are no longer read; rules match paths relative to
  the working directory, and a pattern with a middle `/` is anchored, as in `.gitignore`.
  Directories ignored by `.enmaignore` (including the defaults, e.g. a directory named `x.log`)
  are not descended into, so a `!` rule cannot re-include files below them.
- `--watch-dir` (and `watch_dir` in a watch config) defaults to the working directory: with
  `--working-dir sub` alone, `sub` is watched, not the directory enma was started in.
- Errors go to stderr once; config errors name the file and key; usage errors no longer print the
  help to stdout. `enma <unknown>` is an error. `enma init` reports `Created <file>` and exits
  1 when it cannot write. Some messages are worded differently.
- A new `--logs` file is created with mode `0600`.
- New tagline in the help, the README and the Homebrew description: "Yet another event-driven
  command runner."
- Windows release archives are `.zip` instead of `.tar.gz`; all archives include LICENSE and
  README.md.

### Added

- `enma config validate [--config FILE] [--next N]` checks a config file without running it.
- `--next N` (`enma scheduler ... --next N`, `enma config validate --next N`) lists upcoming
  fire times, with daylight-saving changes handled like a real run.
- A command or build stopped by `--timeout` is reported as `timed out after <d> (--timeout)`
  instead of only `signal: killed`.
- `-C` as the short form of `--check-content-diff` (it was documented but rejected).
- SECURITY.md, CONTRIBUTING.md, this changelog, issue and pull request templates.

### Fixed

- PID files are written atomically and removed only while they hold enma's own pid; a stale
  file is replaced.
- Process shutdown, retries and concurrency limits across hotload, watch and scheduler
  (no double wait, no process left behind, no start after shutdown).
- Watching: new directories, symlinks, unreadable directories and ignore rules (`!dir/`
  re-includes the directory's contents).
- A config file that cannot be loaded no longer reports `<nil>`.
- Help texts and completions list the real flags: `enma init --file-name` (`-f`), `-g` is
  `--ignore-file-regex` and `-G` `--ignore-dir-regex`; the combined completion script can be
  sourced by bash.
- `make release-build` embeds the version, builds arm64 and names Windows binaries `.exe`.

### Known issues

- macOS / BSD (known limitation, accepted and monitored, not fixed): when a directory is
  created while enma runs and files are added to it right away, files added to it afterwards
  can, rarely, be missed until enma is restarted; one file descriptor can also stay open after
  that directory is deleted. The first is caused by a race
  in fsnotify's kqueue backend (reproduced with fsnotify alone); the second is presumed to have
  the same cause. Not observed on Linux, which is not a proof that Linux cannot be affected.
  See README "What is watched".
