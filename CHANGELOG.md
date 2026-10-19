# Changelog

All notable changes to enma are listed here. Releases up to v1.2.10 are described on the
[Releases page](https://github.com/magicdrive/enma/releases).

## Unreleased

### Changed (behaviour differs from v1.2.10)

- Placeholders are no longer pasted into the command text: the changed file's path is passed
  to `sh -c` as the argument `$1` and each `{}` becomes a quoted reference to it (see README
  "Placeholder and the shell"). A file name can no longer inject shell code. Inside a command,
  `$0` is `enma`, `$1` is the path and `$#` is 1.
- Builds, commands and the daemon run in their own process group, and stopping one stops the
  whole group (SIGTERM, then SIGKILL after the grace period).
- A plain `enma` (running the config file found in the current directory) exits with a non-zero
  status when it fails, instead of 0.
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

- Scheduler mode: `enma scheduler --cron "<6-field cron>" --command ...` for one job, and any
  number of jobs from a config file (`[subcommand.scheduler]`, `[[subcommand.scheduler.jobs]]`).
  Time zones and daylight saving, per-job timeouts, retries and working directories, no
  overlapping runs of a job, and a global concurrency limit (README "Scheduler Mode").
- `start-at-once` (`--start-at-once`, TOML `start_at_once`) for hotload and watch: a command run
  once at startup, before the first build or the monitoring.
- `enma config validate [--config FILE] [--next N]` checks a config file without running it.
- `--next N` (`enma scheduler ... --next N`, `enma config validate --next N`) lists upcoming
  fire times, with daylight-saving changes handled like a real run.
- A command or build stopped by `--timeout` is reported as `timed out after <d> (--timeout)`
  instead of only `signal: killed`.
- `-C` as the short form of `--check-content-diff` (it was documented but rejected).
- An unknown key in a `[subcommand.hotload]` / `[subcommand.watch]` section is reported as a
  warning on stderr (it is still ignored); the scheduler section rejects it.
- SECURITY.md, CONTRIBUTING.md, this changelog, issue and pull request templates.
- Fish shell completion (`completions/fish/enma.fish`; Homebrew installs it), and a README
  section on installing the completions.

### Security

- File paths reach commands as an argument, never as code (see "Changed"); a file name with
  quotes, `;`, `$(...)` or backquotes is passed as exactly one argument.

### Fixed

- PID files are written atomically and removed only while they hold enma's own pid; a stale
  file is replaced.
- Process shutdown, retries and concurrency limits across hotload, watch and scheduler
  (no double wait, no process left behind, no start after shutdown).
- Watching: new directories, symlinks, unreadable directories and ignore rules (`!dir/`
  re-includes the directory's contents).
- A config file that cannot be loaded no longer reports `<nil>`.
- Windows: a path holding `{`, `}` or `'` (and a command holding `\\`) reached the command
  changed, because the Git for Windows / MSYS2 `sh` re-parses its command line; the script and
  the path are now passed to `sh` in the environment there.
- `hotload` without a placeholder in its build commands builds and restarts the daemon once for a
  directory that appears with files in it, instead of once per file; an empty directory, or one
  holding only ignored files, no longer causes a build (unless it replaces a directory that was
  moved away or removed, and that change passes the watch rules). For that one build `$1` is the
  directory. With a placeholder each file still has its own build.
- Stopping commands and the daemon works when enma runs as pid 1 (a container started without
  an init) or as a subreaper: the zombies of processes a command left behind are collected, so
  the stop no longer times out and the daemon is restarted.
- `args_path_style` accepts spaces around its commas (`"dir, base, ext"`).
- The allowed signal names in error messages are listed in a fixed order.
- Help texts and completions list the real flags: `enma init --file-name` (`-f`), `-g` is
  `--ignore-file-regex` and `-G` `--ignore-dir-regex`; the combined completion script can be
  sourced by bash.
- `make release-build` embeds the version, builds arm64 and names Windows binaries `.exe`.
- Shell completions follow the command line: every short flag and alias (`-d`, `-S`, `--cmd`,
  `--abs`, ...), values after a space as well as after `=`, files and directories where a flag
  takes one, and nothing after `--` or an argument. zsh completes on the first TAB of a new shell
  (it did nothing until the second), and `watch -c` completes a command, not a config file. The
  bash completion no longer needs bash-completion (it failed with `_init_completion: command
  not found` without it).

### Known issues

- macOS / BSD (known limitation, accepted and monitored, not fixed): when a directory is
  created while enma runs and files are added to it right away, files added to it afterwards
  can, rarely, be missed until enma is restarted; one file descriptor can also stay open after
  that directory is deleted. The first is caused by a race
  in fsnotify's kqueue backend (reproduced with fsnotify alone); the second is presumed to have
  the same cause. Not observed on Linux, which is not a proof that Linux cannot be affected.
  See README "What is watched".
