# enma

[![CI](https://github.com/magicdrive/enma/actions/workflows/ci.yml/badge.svg)](https://github.com/magicdrive/enma/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/magicdrive/enma)](https://github.com/magicdrive/enma/releases)
[![Go Reference](https://pkg.go.dev/badge/github.com/magicdrive/enma.svg)](https://pkg.go.dev/github.com/magicdrive/enma)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

> Yet another event-driven command runner.

`enma` runs your commands when something happens: it rebuilds and restarts a daemon when files change,
runs a command on each change, or runs jobs on cron schedules. It is a single small binary meant to run
for a long time without surprises.

## ✨ Features

- 📂 Realtime monitoring for directories or files
- 🛠️ Execute build or custom commands on file changes
- 🔁 Hot-reload support with build success detection
- 🚦 Flexible daemon process control with customizable signals (SIGTERM, SIGKILL, etc.)
- 🔗 Supports symlinks
- 🧩 Flexible configuration using TOML files
- 🔍 Ignore patterns with `.enmaignore` (`.gitignore` compatible syntax)
- ⏰ Cron-style scheduler for one or more jobs, with timeouts, retries and a concurrency limit
- 📦 A single self-contained binary for Linux, macOS and Windows

---

## 🚀 Quick Start

### What enma is

It has three modes:

| Mode | Runs | Typical use |
|---|---|---|
| `hotload` | a build on each change, then restarts a long-running daemon | a web server you are developing |
| `watch` | a command on each change (the changed path can be passed to it) | tests, linters, code generation |
| `scheduler` | commands on cron schedules (seconds field, time zones, retries) | periodic jobs without a system cron |

### Install

```bash
go install github.com/magicdrive/enma@latest   # Go 1.25 or later (the newest Go is recommended)
brew install magicdrive/tap/enma               # or Homebrew
```

Pre-built binaries are on the [Releases page](https://github.com/magicdrive/enma/releases).
Upgrading from v1.2.10 or earlier? Some behaviour changed: see [CHANGELOG.md](CHANGELOG.md).

### Shell completion

Completions for bash, zsh and fish are in [`misc/completions`](misc/completions) and in the
`completions/` directory of each release archive. Homebrew installs them; open a new shell
afterwards. bash reads them through bash-completion (`brew install bash-completion@2`); a zsh not
installed by Homebrew needs `$(brew --prefix)/share/zsh/site-functions` in `$fpath` before
`compinit`. Otherwise:

| Shell | Install |
|---|---|
| bash | `source /path/to/completions/bash/enma-completion.bash` in `~/.bashrc` (bash-completion is not needed) |
| zsh | copy `completions/zsh/_enma` into a directory of `$fpath`, before `compinit` runs |
| fish | copy `completions/fish/enma.fish` into `~/.config/fish/completions/` |

`completions/enma-completion.sh` works in bash and zsh alike: source it (in zsh after
`compinit`). They complete the subcommands, each flag of the command line (short and long
spellings), the values of flags with a fixed set of values (`--signal`, `--mode`,
`--default-ignores`, `on`/`off`, `--args-path-style`), and files and directories where a flag
takes one. With bash 3.2 (macOS `/bin/bash`), paths are completed by bash itself: files are
offered for directory and free-text values too, and not after a comma in a list.

### Run it

Run the tests whenever a Go file below the current directory changes:

```bash
enma watch --command "go test ./..." --include-ext .go --timeout 5m
```

Stop it with Ctrl-C. A command (or build) that runs longer than `--timeout` (default: 5 seconds
in watch and hotload, no limit in scheduler) is stopped and reported as timed out, so give slow
commands a longer one. `enma hotload --build "go build -o ./app ." --daemon ./app --timeout 2m`
rebuilds and restarts a server the same way.

### Create a config file

```bash
enma init --mode watch                  # writes Enma.toml (modes: hotload, watch, scheduler)
enma init --mode enmaignore             # writes .enmaignore (paths to leave alone, .gitignore syntax)
```

Edit `Enma.toml`, then run `enma` in that directory (or `enma --config <file>`).

### Check a config file

```bash
enma config validate                    # or: enma config validate --config ./Enma.toml
```

It loads the file exactly like a run does and reports the problems it finds, each line naming
the file: all problems of one check together (for example every invalid value), later checks
once those are fixed.
Nothing is started: no command runs, and no pid or log file is written. Exit code 0 means valid.

### Schedule a job

```bash
enma scheduler --cron "0 */10 * * * *" --command "make sync" --timezone Asia/Tokyo
```

The six cron fields start with seconds. For several jobs, use `enma init --mode scheduler`.

### See when jobs will run

```bash
enma scheduler --cron "0 30 2 * * *" --command "./backup.sh" --timezone Europe/Berlin --next 5
enma config validate --next 5           # every job of a scheduler config file
```

`--next` prints the next fire times of each job, with the time zone and UTC offset, and exits.
They come from the scheduler's own calculation, so daylight-saving changes are handled the
same way: a local time that occurs twice runs once, and one that does not exist that day
(the clocks go forward over it) does not run. See [Time](#time).

### Platforms

Linux and macOS (amd64, arm64) are fully supported. CI runs the tests on Linux (amd64, and arm64
in the extended checks) and macOS (arm64); macOS amd64 is tested by the maintainers. Windows
(amd64) builds work with
limits: commands run with `sh -c`, so a POSIX shell (e.g. from Git for Windows or MSYS2) must be on `PATH`; they
run without process groups, so enma cannot stop processes a command leaves behind, and enma
itself stops on Ctrl-C or SIGTERM only. File watching uses inotify on Linux and kqueue on
macOS (one file descriptor per watched file, and a rare race for new directories; see
[What is watched](#what-is-watched)).
Release archives are also built for linux/386, windows/386 and windows/arm64; those builds are
not tested.

### Learn more

- [Hotload mode](#-hotload-mode), [Watch mode](#-watch-mode), [Scheduler mode](#-scheduler-mode)
- [File filtering](#-file-filtering-order) and [`.enmaignore`](#-example-enmaignore)
- [Output, logs and exit codes](#-output-logs-and-exit-codes)
- [Full command reference](#-full-command-reference) (`enma --help`, `enma <mode> --help`)

---

## 🔥 Hotload Mode

Use this when you want to automate build and daemon restarts.

### Example Config (`Enma.toml`)

```toml
[subcommand]
use = "hotload"

[subcommand.hotload]
daemon = "./my-app"
build = "go build -o my-app ."
watch_dir = ["./cmd", "./internal"]
include_ext = [".go"]
```

`enma init --mode hotload` writes a template with every key.

### Run

```bash
enma hotload --daemon ./my-app --build "go build -o my-app ."
```

---

## 👀 Watch Mode

Executes commands on file changes without restarting daemons.

### Example Config (`Enma.toml`)

```toml
[subcommand]
use = "watch"

[subcommand.watch]
command = "make test"
watch_dir = ["./pkg", "./lib"]
```

`enma init --mode watch` writes a template with every key.

### Run

```bash
enma watch --command "make test file={}"
```

---

## 🏁 Run a command once at startup (`start-at-once`)

Both modes can run one command once when enma starts, before anything else:

```bash
enma watch --start-at-once "make prepare" --command "make test"
enma hotload --start-at-once "make prepare" --daemon ./my-app --build "go build -o my-app"
```

```toml
[subcommand]
use = "watch"

[subcommand.watch]
start_at_once = "make prepare"   # `start-at-once` is accepted as well
command = "make test"
```

- **Order** — watch: `start-at-once` → file monitoring. hotload: `start-at-once` → file
  monitoring set up → the first build (`--build-at-start`) → daemon. Monitoring starts only
  after it has finished, so files it changes do not trigger a build or command. (As before,
  a change during the first build may still trigger a build after it.)
- **Once** — it runs at most once per enma process: never again on a file change, a retry or a
  daemon restart. It is independent of `pre-build` / `build` / `post-build` and of
  `pre-cmd` / `command` / `post-cmd`, and does not run `command` at startup.
- **Failure** — when it exits non-zero (or cannot be run), enma reports the error and does not
  start the build, the daemon or the monitoring.
- It runs in `--working-dir` with `sh -c`. There is no changed file, so the placeholder is
  **not** replaced: `{}` reaches the shell as written.
- `--timeout`, `--retry` and `--delay` do not apply to it: it may take as long as it needs, and
  Ctrl+C (or SIGTERM) stops it and its process group like any other command
  (SIGTERM, then SIGKILL after the grace period; a second signal kills at once).
- It must not leave processes behind: what is still running in its process group when it
  returns is stopped (SIGTERM, then SIGKILL), so it cannot start a background service
  (in hotload, use `--daemon` for that). On Windows enma cannot see a command's descendants.
- Not set or empty: nothing runs, as before.

---

## ⏰ Scheduler Mode

Runs commands on cron schedules. One job from the command line, any number from a config file.

```bash
enma scheduler --cron "0 */10 * * * *" --command "make sync" --name sync \
  --timezone Asia/Tokyo --timeout 30s --retries 3 --retry-delay 10s --max-concurrent 4
```

```toml
[subcommand]
use = "scheduler"

[subcommand.scheduler]
timezone = "Asia/Tokyo"   # default: the OS local time zone
max_concurrent = 4        # default: 4

[[subcommand.scheduler.jobs]]
name = "sync"             # required, unique
cron = "0 */10 * * * *"   # required
command = "make sync"     # required
working_dir = "./project" # default: the directory enma was started in
timeout = "30s"           # default: no limit
retries = 3               # default: 0
retry_delay = "10s"       # default: 10s

[[subcommand.scheduler.jobs]]
name = "backup"
cron = "0 0 2 * * *"
command = "./backup.sh"
```

`enma init --mode scheduler` writes a commented template. The command line and a config file
are never merged: `enma scheduler ...` uses its flags only, `enma` / `enma --config <file>` the file only.
Every setting is checked before the first job starts; an error names the job and the field, and
enma exits with 1. Unknown keys under `[subcommand.scheduler]` are errors.

### Cron expressions

Six fields, seconds first: `second minute hour day-of-month month day-of-week`
(`0-59 0-59 0-23 1-31 1-12 0-6`, Sunday is `0`). Each field is a list of `*`, `*/n`, `n`, `a-b`
or `a-b/n`. Not supported: the 5-field form, names (`JAN`, `SUN`), `?`, `L`, `W`, `#`, `@every`
and other `@` descriptors.

Day of month and day of week: a field is *restricted* unless it starts with `*` — so `*/2` is
unrestricted (like `*`), while `1-31/2` (the same days) is restricted. When both fields are
restricted, a day matches if **either** matches; otherwise both must match (the unrestricted one
matches every day). This is the classic cron rule; treating `*/n` as unrestricted is how enma
defines it.

| expression          | runs at 09:00 on (from 2026-10-31)                  |
|---------------------|-----------------------------------------------------|
| `0 0 9 * * 1`       | Mondays: Nov 2, 9, 16, …                            |
| `0 0 9 1 * *`       | the 1st: Nov 1, Dec 1, …                            |
| `0 0 9 1 * 1`       | the 1st **or** Mondays: Nov 1, 2, 9, 16, 23, 30, Dec 1, … |
| `0 0 9 */2 * 1`     | odd days **that are** Mondays: Nov 9, 23, Dec 7, …  |
| `0 0 9 1-31/2 * 1`  | odd days **or** Mondays: Oct 31, Nov 1, 2, 3, 5, …  |

### Time

- All jobs use one time zone (`timezone`, default the OS local time zone).
- Nothing runs at start, not even when enma starts exactly at a fire time, and fire times that
  passed while enma was stopped are not run later.
- Daylight saving: a local time that does not exist that day is not run. When the clocks go
  back, a local date-time (date and wall-clock time in the scheduler's time zone, whatever the
  UTC offset) happens twice but is one fire opportunity: once enma has met its first instance —
  run, failed, skipped (`job_busy`, `capacity_unavailable`) or missed (late, or jumped over by a
  clock change) — the second instance does not fire. If enma was started between the two
  instances (or exactly at the first one, which does not run at start), the second one fires
  (the first one was not met while enma was running). Retries are
  not fires and are not affected. Nothing of this is stored: a restarted enma starts afresh.
- If the system clock is set forward by more than a second (or the machine slept), the fire
  times between the old and the new time are not run: one `skip reason=clock_jump` event covers
  them (`scheduled=` the first, `until=` about the new time). enma cannot see exactly when the
  clock was set: fire times shortly before it noticed (usually within 15 seconds) are treated
  as on the new time line and may still run. If the clock is set back, fire times already run do not run
  again (also inside a repeated daylight-saving hour). enma looks at the wall clock at least
  every 15 seconds, so after a clock change a fire may be up to that late.
- A fire found due more than 30 seconds late (enma could not run, e.g. it was stopped with
  SIGSTOP) is not run. When several fire times of a job are due at once, only the newest runs.
  Either way the others are one `skip reason=missed` event with `count=` (`count=<n>+` after a
  very long stall: at least that many).

### Runs, retries and concurrency

- A job never runs twice at once. A fire while the job's previous run (or its retry wait) is not
  over is skipped (`reason=job_busy`). Fires are never queued.
- `max_concurrent` limits the attempts running at the same time over all jobs. A fire that finds
  no free slot is skipped (`reason=capacity_unavailable`). When several jobs fire at the same
  second, the free slots go to them in the order of the config file.
- An attempt fails on a non-zero exit, when the command cannot be started, or on `timeout` (the
  limit of one attempt; its process group is killed). After a failure the job runs again
  `retry_delay` after the attempt ended, up to `retries` times (`retries = 3`: at most 4 attempts).
  Success or a shutdown is never retried.
- During a retry wait the job stays busy but holds no slot. If no slot is free when the retry is
  due, it is dropped and the run ends (`retry_capacity_unavailable`). Retries due at the same time
  as fires are handled first, in config order.
- A failing job never stops the scheduler or the other jobs.
- Each job runs in its own `working_dir` with `sh -c`, in its own process group; nothing it starts
  is left behind (as with the other modes). Its output goes to enma's stdout / stderr, so the
  output of jobs running at the same time interleaves; the event lines name the job.

### Stopping

SIGINT / SIGTERM: no new fires or retries, then every running attempt gets SIGTERM at the same
time and SIGKILL after the grace period (5s); enma exits with 0 (1 if something could not be
stopped). A second signal kills at once (exit 1).

### Log events

One line per event: `⏰ scheduler event=<event> job="<name>" attempt=<n> scheduled=<time>
start=<time> end=<time> duration=<d> exit_code=<n> reason=<reason> error="<error>"` (fields that
apply). Events: `scheduler_start`, `scheduler_stop`, `job_start`, `job_success`, `job_failed`
(`reason=exit|start_failed|leftover`), `job_timeout`, `job_canceled`, `retry_scheduled`
(`retry_at=`), `retry_start`, `retry_success`, `retries_exhausted`, `retry_capacity_unavailable`,
`retry_canceled` (a retry wait dropped at shutdown), `skip`
(`reason=job_busy|capacity_unavailable|clock_jump|missed`, with `until=` / `count=`). Commands
are not logged. Processes a job leaves behind are stopped and logged with the job's name.
`--logs` / `--pid` (TOML `logs` / `pid` under `[subcommand.scheduler]`) work as in the other modes.

---

## 🔐 Placeholder and the shell

Commands run with `sh -c`. The changed file's path is **not** pasted into the command text:
it is passed to the shell as the positional parameter `$1`, copied to the variable
`__enma_path` at the start of the script, and each placeholder becomes a reference to that
variable that fits its quoting:

| You write             | The shell runs                              |
|-----------------------|---------------------------------------------|
| `echo {}`             | `echo "${__enma_path}"`                     |
| `make test file={}`   | `make test file="${__enma_path}"`           |
| `echo "changed: {}"`  | `echo "changed: ${__enma_path}"`            |
| `echo 'changed: {}'`  | `echo 'changed: '"${__enma_path}"''`        |

(the script starts with `__enma_path="$1"; `). So a file name containing spaces, quotes, `;`,
`$(...)` or backquotes reaches the command as exactly one argument, and the shell never parses
it as code. Notes:

- `$0` is `enma`, `$1` is the path and `$#` is 1 inside the command.
- On Windows the command line of a program is one string that the POSIX shell (Git for Windows,
  MSYS2) splits again with its own rules for quotes, braces and patterns. So there enma passes
  the script and the path to `sh` in the environment (`__ENMA_SCRIPT`, `__ENMA_ARG1`; removed
  before the command runs), and the command gets them unchanged.
- An empty placeholder (`--placeholder ""`) disables substitution: no command has a placeholder.
- The placeholder is left as it is where the shell would not expand a variable: after a
  backslash (`\{}`), in `#` comments and in quoted here-documents (`<<'EOF'`). Such a
  placeholder is plain text and does not count as one: `hotload` still runs the build at
  start, and a `--pre-build` / `--post-build` without a real placeholder still runs then.
- In an unquoted here-document the placeholder expands to the path. In a quoted
  here-document (`<<'EOF'`) it is not replaced, by design: the body is literal text.
- Write a command substitution that starts with a subshell as `$( (cmd) ...)`, with a space,
  as POSIX requires; `$((` always starts arithmetic. Likewise write a nested subshell as
  `( (cmd) )` rather than `((cmd))`.
- `$'...'` quoting is a bash/zsh feature; `sh` implementations without it (e.g. dash) print
  it differently, with or without enma.

### ⚠️ Not covered: running the path as code again

The protection above covers the shell that enma starts. **It is not guaranteed** once the
command hands the path to something that parses it as code again: a second shell or an
interpreter receives the path *inside its own source text*, and a file name such as
`x'; rm -rf ~; '.go` is then executed there. This includes:

- `eval {}`, and arithmetic such as `[[ {} -eq 0 ]]` or `$(( {} ))`;
- a quoted string given to another shell: `sh -c 'cat {}'`, `bash -c "..."`,
  `docker exec c sh -c '... {}'`, `xargs sh -c '...'`;
- remote commands: `ssh host 'cat {}'` (ssh joins its arguments into a remote shell command);
- interpreter source: `awk '... "{}" ...'`, `perl -e '...{}...'`, `python -c '...{}...'`.

Pass the path as an **argument** instead of inside the program text:

```bash
# another shell: the path is $1 of that shell
enma watch --command "sh -c 'cat \"\$1\"' sh {}"
# interpreters: read it from argv
enma watch --command "awk '{ print FILENAME \": \" \$0 }' {}"
enma watch --command "perl -ne 'print \"\$ARGV: \$_\"' {}"
enma watch --command "python3 -c 'import sys; print(open(sys.argv[1]).read())' {}"
# ssh: quote the path for the remote shell yourself (bash printf %q)
enma watch --command "bash -c 'ssh host \"cat -- \$(printf %q \"\$1\")\"' bash {}"
```

## 🔍 File Filtering Order

When monitoring file changes, Enma filters target files based on the following order:

1. If the file does **not match `--pattern-regex`**, it is excluded.
2. If the file **matches `--exclude-dir` or `--exclude-ext`**, it is excluded.
3. If the file **matches `--ignore-dir-regex` or `--ignore-file-regex`**, it is excluded.
4. If the file is **listed in the `.enmaignore` file**, it is excluded.
5. If `--include-ext` is specified and the file **does not match any of the extensions**, it is excluded.
6. Files that pass all the above filters are considered as **watch targets**.


### 💡 Why This Order Makes Sense

This filtering order is designed to be both **efficient** and **intuitive**:

- **Early exclusion improves performance** by skipping unnecessary processing for files that obviously shouldn't be watched.
- `--pattern-regex` acts as a **top-level filter**, giving users full control over what files are even considered.
- `--exclude-*` and `--ignore-*` rules eliminate unwanted files using both simple and flexible patterns.
- The `.enmaignore` file gives users a familiar, Git-style way to exclude files explicitly.
- Finally, `--include-ext` allows users to **narrow down** the remaining files by extension, but only if they choose to use it.

This layered approach ensures clarity in behavior while keeping Enma fast and customizable.

### What is watched

- `--watch-dir` defaults to the working directory (`--working-dir`, or where enma was started).
  A watch config without `watch_dir` watches the working directory as well.
- `--exclude-dir` and `.enmaignore` rules look at paths **relative to the working directory**
  (for a watch directory outside it: relative to that watch directory). The directories
  *above* the project never match, so a project inside `~/.enma/`, `vendor/` or `tmp/` is
  watched normally, while `.enma/`, `.git/`, ... inside it are still ignored.
- `.enmaignore` uses `.gitignore` syntax: `*`, `?`, `**`, character classes (`*.py[cod]`,
  `[!a]x`), `\` escapes (also `\ ` for a trailing space), `!` negation, the last matching line
  wins; a leading or middle `/` anchors a pattern to the working directory (`/build` is the
  top-level `build` only, `vendor/bundle/` the top-level `vendor/bundle`). Not supported:
  POSIX classes such as `[[:alpha:]]`. A pattern ending in `/` also matches a file of that
  name, so `!dir/` re-includes files named `dir` too. `!dir/` also re-includes everything
  below `dir/` (`*.log` + `!logs/` watches `logs/a.log` and `logs/sub/b.log`), and the
  whitelist idiom `*` + `!*/` re-includes everything. As in git, nothing inside an ignored directory can be re-included
  (`build/` + `!build/keep/` keeps `build/keep` ignored; use `build/*` + `!build/keep/`).
  An invalid line is reported and skipped. enma reads the `.enmaignore` of the working
  directory and the files given with `--enmaignore`; it does not search parent directories.
- `--ignore-dir-regex` is matched against the absolute directory path.
- A subdirectory that cannot be read is reported and left out; the rest is watched.
- Symlinked directories are not followed when monitoring starts. A symlink created later is
  followed only if it points inside the watch directories (one pointing elsewhere is reported).
- Several files changed within the 300 ms debounce window run the command (or build) once
  **per file**, each with its own path for the placeholder.
- A directory that appears while enma runs (created, moved in, unpacked) is watched, and the
  files already in it count as changed — one run per file, as above — unless the watch rules
  reject the directory. Exception, `hotload` without a placeholder in `--pre-build`, `--build`
  and `--post-build`: the new directory is one change. Its files that pass the watch rules,
  including those created in it before the debounce window ends, give one build and one daemon
  restart. For that build `$1` is the directory, not a file: also with `--include-ext`, and
  `--args-path-style` applies to it (`dirname` gives its parent). A directory with no such file
  (empty, or only ignored files) gives none, unless it replaces a directory that was moved away
  or removed and that change passes the watch rules (a directory name does not pass
  `--include-ext` or `--pattern`).
  With `--check-content-diff`, it is built when one of its files changed.
- macOS (kqueue) uses one file descriptor per watched file: for large trees raise the limit
  (`ulimit -n`) or narrow `--watch-dir`; files that cannot be watched are reported.
- Known limitation on macOS and BSD (kqueue), accepted and monitored, not fixed: when files are
  created in a directory right after the directory itself appears, enma can, rarely, stop
  noticing files added to that new directory: files and subdirectories created in it later are
  then not seen until enma is restarted. Files that existed when the directory was picked up
  are not affected. From the code, the same race is presumed possible (not observed) for a
  directory being added while its parent changes, e.g. at startup.
  The cause is a race in the file-watching library (fsnotify, kqueue backend) between its own
  registration of a new directory and enma's: it was reproduced with fsnotify alone. A related
  symptom, one file descriptor kept open after such a directory is deleted, is presumed to have
  the same cause (seen only in enma). It has not been observed on Linux (inotify backend, which
  registers directories differently); that is not a proof that Linux cannot be affected. This
  will be revisited once fsnotify fixes it.

### Processes a command leaves behind

`pre-build` / `build` / `post-build` and `pre-cmd` / `command` / `post-cmd` run with `sh -c` in
their own process group. When the command returns, whatever it left running in that group
(e.g. `something &`) is stopped: SIGTERM, then SIGKILL after the grace period (5s). Use
`--daemon` (hotload) for a process that should keep running. Only a group that cannot be
stopped at all fails the step. On Windows enma cannot see a command's descendants.

In a container, run enma with an init process (`docker run --init`, or tini). As pid 1 enma
collects the processes of a command's group, but a process that moved to another session or
group (e.g. `setsid`, double fork) would stay a zombie.

### Changes since v1.2.10

Some behaviour differs from v1.2.10 and earlier (processes left behind are stopped,
`.enmaignore` lookup, the default watch directory, error output, ...): see
[CHANGELOG.md](CHANGELOG.md).

### PID file (`--pid`)

The file holds enma's process id (decimal, nothing else). enma writes it whole before it starts
anything, and removes it when it exits on a signal or when the start fails. It removes it only
while it still holds its own pid, so a file another enma has written since is kept. When an
existing file names a process that is still running, enma replaces it and prints a warning.
A pid file cannot be removed after `kill -9`, a crash or a power loss; the next start replaces
it. The pid in a stale file may by then belong to an unrelated process.

---

## 📋 Output, logs and exit codes

- **Errors** go to stderr, once, and name what is wrong: errors in a config file name the file
  (with the line and column of a TOML syntax error) and the key, and scheduler errors the job
  and field; missing flags are named. A directory that cannot be used when a run starts is
  reported with its path and the system's reason (`enma config validate` names the key too).
  An unknown key in a `[subcommand.hotload]` / `[subcommand.watch]` section is reported as a
  warning and ignored; the scheduler section rejects it.
- **enma's own log lines** (with a timestamp) go to stderr; with `--logs <file>` they go to
  stdout and to the file instead. A new log file is created with mode `0600`; an existing one
  keeps its mode (e.g. `0644` from an older enma), and a symlink at that path is followed.
- **Commands' output** (build, daemon, command, start-at-once, scheduler jobs) goes straight to
  enma's stdout / stderr; it is not copied into the `--logs` file, and the output of commands
  running at the same time interleaves (scheduler event lines name the job). To keep a job's
  output apart, redirect it in the command: `command = "./report.sh >> report.log 2>&1"`.
- enma never logs the commands it runs or the environment; they may hold secrets. Log lines
  name files, jobs and errors.
- If the reader of enma's output goes away (e.g. `enma watch ... | head`), enma keeps running
  and keeps writing the `--logs` file; stop it with a signal as usual. The commands share that
  output, so one that writes to it ends with SIGPIPE as usual (a daemon too).

| Exit code | When |
|---|---|
| `0` | `--help`, `--version`, `init` succeeded; a run stopped by SIGINT / SIGTERM / SIGHUP whose shutdown completed |
| `1` | any error: config file, invalid setting, unknown subcommand, missing required flag, a start that failed (watch directory, `start-at-once`, pid file), `init` that could not write; a shutdown that could not stop everything; a forced stop (second signal) |
| `2` | a command line the flag parser rejects (unknown flag, a flag without its value) |

A failing scheduler job does not end enma or change its exit code: it is reported as an event.

---

## 🧾 Full Command Reference

### Global Options

| Option                         | Description                                                                                     |
|--------------------------------|-------------------------------------------------------------------------------------------------|
| `-h`, `--help`                 | Show help message and exit                                                                      |
| `-v`, `--version`              | Show version                                                                                    |
| `-c`, `--config`               | Specify config file. Default: `./Enma.toml`, `./.enma.toml`, `./.enma/enma.toml` or `./.config/enma.toml` |

---

### `enma init`

| Option                        | Description                                               |
|-------------------------------|-----------------------------------------------------------|
| `-h`, `--help`                | Show help message and exit                                |
| `-m`, `--mode`                | Mode for config file: `hotload`, `watch`, `scheduler` or `enmaignore` (default: `hotload`) |
| `-f`, `--file-name <filename>` | Config filename to create (default: `./Enma.toml`, `.enmaignore` for that mode) |

---

### `enma hotload`

| Option                                     | Description                                                                |
|--------------------------------------------|----------------------------------------------------------------------------|
| `-h`, `--help`                             | Show help message and exit                                                 |
| `-d`, `--daemon <command>`                 | Daemon command to run (required)                                           |
| `-b`, `--build <command>`                  | Command to build the daemon (required)                                     |
| `-S`, `--signal <signal>`                  | Signal to stop the daemon command (optional. default: SIGTERM)             |
| `--start-at-once <command>`                | Command run once at startup, before the first build (optional)             |
| `-w`, `--watch-dir <dir_name>`             | Watch directories (comma-separated, optional. default: the working dir)     |
| `-p`, `--pre-build <command>`              | Command to run before build (optional)                                     |
| `-P`, `--post-build <command>`             | Command to run after build (optional)                                      |
| `-W`, `--working-dir <dir_name>`           | Working directory (default: `$PWD`)                                        |
| `-I`, `--placeholder <placeholder>`        | Placeholder in command for changed file (default: `{}`)                    |
| `-s`, `--args-path-style <style>`          | filepath-style where event in command. <br> (optional. default: `dir,base,ext`) |
| `-B`, `--build-at-start <on\|off>`         | Run a build before starting daemon for the first time. <br> If there is a placeholder, it will be ignored. |
| `-C`, `--check-content-diff <on\|off>`     | Fires only when the file contents are changed. (optional. default: `on`)   |
| `-A`, `--abs`, `--absolute-path <on\|off>` | Use absolute path in placeholder (optional. default: `on`)                 |
| `-t`, `--timeout <time>`                   | Timeout for build command (default: `5sec`)                                |
| `-l`, `--delay <time>`                     | Delay after build command (default: `0sec`)                                |
| `-r`, `--retry <number>`                   | Retry count (default: `0`)                                                 |
| `-x`, `--pattern-regex <regex>`            | Regex pattern to watch (optional)                                          |
| `-i`, `--include-ext <ext>`                | File extensions to include (comma-separated, optional)                     |
| `-g`, `--ignore-file-regex <regex>`        | Regex to ignore files (optional)                                           |
| `-G`, `--ignore-dir-regex <regex>`         | Regex to ignore directories (optional)                                     |
| `-e`, `--exclude-ext <ext>`                | File extensions to exclude (comma-separated, optional)                     |
| `-E`, `--exclude-dir <dir_name>`           | Directories to exclude (comma-separated, optional)                         |
| `-D`, `--default-ignores <max\|min\|none>` | Built-in ignore rules to load (optional. default: `minimal`)               |
| `-n`, `--enmaignore <filename>`            | enma ignore file(s) (comma-separated, optional. default: `./.enmaignore`)  |
| `--logs <log_file_path>`                   | Log file path (optional)                                                   |
| `--pid <pid_file_path>`                    | PID file path (optional)                                                   |

---

### `enma watch`

| Option                                     | Description                                                                |
|--------------------------------------------|----------------------------------------------------------------------------|
| `-h`, `--help`                             | Show help message and exit                                                 |
| `-c`, `--command`, `--cmd <command>`       | Command to run on file change (required)                                   |
| `--start-at-once <command>`                | Command run once at startup, before file monitoring starts (optional)      |
| `-p`, `--pre-cmd <command>`                | Command to run before main command (optional)                              |
| `-P`, `--post-cmd <command>`               | Command to run after main command (optional)                               |
| `-w`, `--watch-dir <dir_name>`             | Watch directories (comma-separated, optional. default: the working dir)     |
| `-W`, `--working-dir <dir_name>`           | Working directory (optional. default: `$PWD`)                              |
| `-I`, `--placeholder`                      | Placeholder in command for changed file (default: `{}`)                    |
| `-s`, `--args-path-style <style>`          | filepath-style where event in command. <br> (optional. default: `dir,base,ext`) |
| `-C`, `--check-content-diff <on\|off>`     | Fires only when the file contents are changed. (optional. default: `on`)   |
| `-A`, `--abs`, `--absolute-path <on\|off>` | Use absolute path in placeholder (optional. default: `on`)                 |
| `-t`, `--timeout <time>`                   | Timeout for command (default: `5sec`)                                      |
| `-l`, `--delay <time>`                     | Delay after command (default: `0sec`)                                      |
| `-r`, `--retry <number>`                   | Retry count (default: `0`)                                                 |
| `-x`, `--pattern-regex <regex>`            | Regex pattern to watch (optional)                                          |
| `-i`, `--include-ext <ext>`                | File extensions to include (comma-separated, optional)                     |
| `-g`, `--ignore-file-regex <regex>`        | Regex to ignore files (optional)                                           |
| `-G`, `--ignore-dir-regex <regex>`         | Regex to ignore directories (optional)                                     |
| `-e`, `--exclude-ext <ext>`                | File extensions to exclude (comma-separated, optional)                     |
| `-E`, `--exclude-dir <dir_name>`           | Directories to exclude (comma-separated, optional)                         |
| `-D`, `--default-ignores <max\|min\|none>` | Built-in ignore rules to load (optional. default: `minimal`)               |
| `-n`, `--enmaignore <filename>`            | enma ignore file(s) (comma-separated, optional. default: `./.enmaignore`)  |
| `--logs <log_file_path>`                   | Log file path (optional)                                                   |
| `--pid <pid_file_path>`                    | PID file path (optional)                                                   |

---

### `enma scheduler`

| Option                                     | Description                                                                |
|--------------------------------------------|----------------------------------------------------------------------------|
| `-h`, `--help`                             | Show help message and exit                                                 |
| `--cron <expression>`                      | 6-field cron expression (required)                                         |
| `--command <command>`                      | Command to run (required)                                                  |
| `--name <name>`                            | Job name (default: `default`)                                              |
| `--working-dir <dir_name>`                 | Working directory of the command (default: `$PWD`)                         |
| `--timezone <zone>`                        | Time zone of the cron expression (default: OS local time)                  |
| `--timeout <time>`                         | Time limit of one attempt (default: none)                                  |
| `--retries <number>`                       | Retries after a failed attempt (default: `0`)                              |
| `--retry-delay <time>`                     | Wait between a failed attempt and its retry (default: `10s`)               |
| `--max-concurrent <number>`                | Maximum attempts running at once (default: `4`)                            |
| `--next <number>`                          | List the next N fire times of the job and exit; nothing runs (at most 100) |
| `--logs <log_file_path>`                   | Log file path (optional)                                                   |
| `--pid <pid_file_path>`                    | PID file path (optional)                                                   |

---

### `enma config validate`

| Option                                     | Description                                                                |
|--------------------------------------------|----------------------------------------------------------------------------|
| `-h`, `--help`                             | Show help message and exit                                                 |
| `-c`, `-f`, `--config <file>`              | Config file to check (default: the file a plain `enma` would run)          |
| `--next <number>`                          | Scheduler configs: list the next N fire times of each job (at most 100)    |

Loads the file like a run does and reports the problems it finds, each line naming the file (all
problems of one check together; a later check runs once the earlier ones pass). Nothing is
started and nothing is written. Exit code: `0` valid, `1` invalid, `2` unknown flag.

---

## 🗂 Example `.enmaignore`

The syntax of .enmaignore is a compatible syntax of [.gitignore](https://git-scm.com/docs/gitignore).

```
# =============================
# VCS / Version Control
# =============================
.git/
.hg/
.svn/

# =============================
# Editors / IDEs
# =============================
.idea/
.vscode/
*.code-workspace
*.sublime-project
*.sublime-workspace
```

---

## Security

enma runs the commands you give it with `sh -c`, as the user that started it. Treat a config
file like a shell script: run `enma` / `enma --config` only on files you trust, and note that a
plain `enma` reads `Enma.toml` (or `.enma.toml`, `.enma/enma.toml`, `.config/enma.toml`) from
the current directory. `enma config validate` checks a file without running anything.
File paths reach commands safely as described in
[Placeholder and the shell](#-placeholder-and-the-shell), with the limits listed there.
Report vulnerabilities privately as described in [SECURITY.md](SECURITY.md).

## Contributing

Bug reports and pull requests are welcome. See [CONTRIBUTING.md](CONTRIBUTING.md) for how to
build and test, and [CHANGELOG.md](CHANGELOG.md) for what changed.

---

## Author

Copyright (c) 2025 Hiroshi IKEGAMI

---

## License

This project is licensed under the [MIT License](https://github.com/magicdrive/enma/blob/main/LICENSE)
