# enma

> Yet another integration software with file monitoring.

`enma` is a file-watching tool that monitors specified files or directories and automatically executes commands or rebuilds and restarts daemons.<br>
It's designed to supercharge your development and automation workflows.

## ✨ Features

- 📂 Realtime monitoring for directories or files
- 🛠️ Execute build or custom commands on file changes
- 🔁 Hot-reload support with build success detection
- 🚦 Flexible daemon process control with customizable signals (SIGTERM, SIGKILL, etc.)
- 🔗 Supports symlinks
- 🧩 Flexible configuration using TOML files
- 🔍 Ignore patterns with `.enmaignore` (`.gitignore` compatible syntax)
- 🧪 Ideal for CI/CD and local development

---

## 🚀 Quick Start

### 1. Installation

```bash
go install github.com/magicdrive/enma@latest
```

You can install enma using `homebrew`:

```bash
brew install magicdrive/tap/enma
```

Alternatively, you can download a pre-built binary from the [Releases page](https://github.com/magicdrive/enma/releases).

### 2. Initialize project

```bash
enma init
```

This generates `Enma.toml` and `.enmaignore`.

> You can specify the mode or config file name:
>
> ```bash
> enma init --mode watch --file ./myconfig.enma.toml
> ```

---

## 🔥 Hotload Mode

Use this when you want to automate build and daemon restarts.

### Example Config (`Enma.toml`)

```toml
name = "my-app"
daemon = "./my-app"
build = "go build -o my-app main.go"
watch-dir = ["./cmd", "./internal"]
```

### Run

```bash
enma hotload --daemon ./my-app --build "go build -o my-app main.go"
```

---

## 👀 Watch Mode

Executes commands on file changes without restarting daemons.

### Example Config (`Enma.toml`)

```toml
command = "make test"
watch-dir = ["./pkg", "./lib"]
```

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

---

## 🧾 Full Command Reference

### Global Options

| Option                         | Description                                                                                     |
|--------------------------------|-------------------------------------------------------------------------------------------------|
| `-h`, `--help`                 | Show help message and exit                                                                      |
| `-v`, `--version`              | Show version                                                                                    |
| `-c`, `--config`               | Specify config file. Default: `./Enma.toml`, `./.enma.toml`, or `./.enma/enma.toml`            |

---

### `enma init`

| Option                        | Description                                               |
|-------------------------------|-----------------------------------------------------------|
| `-h`, `--help`                | Show help message and exit                                |
| `-m`, `--mode`                | Mode for config file: `hotload`,`watch` or `enmaignore` (default: `hotload`) |
| `-f`, `--file <filename>`     | Config filename to create (default: `./Enma.toml`)        |

---

### `enma hotload`

| Option                                     | Description                                                                |
|--------------------------------------------|----------------------------------------------------------------------------|
| `-h`, `--help`                             | Show help message and exit                                                 |
| `-d`, `--daemon <command>`                 | Daemon command to run (required)                                           |
| `-b`, `--build <command>`                  | Command to build the daemon (required)                                     |
| `-S`, `--signal <singal>`                  | Define signal to stop the daemon command. (optional. default: SIGTERM)     |
| `--start-at-once <command>`                | Command run once at startup, before the first build (optional)             |
| `-w`, `--watch-dir <dir_name>`             | Watch directories (comma-separated, optional. default: `$PWD`)             |
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
| `-g`, `--ignore-dir-regex <regex>`         | Regex to ignore directories (optional)                                     |
| `-G`, `--ignore-file-regex <regex> `       | Regex to ignore files (optional)                                           |
| `-e`, `--exclude-ext <ext>`                | File extensions to exclude (comma-separated, optional)                     |
| `-E`, `--exclude-dir <dir_name>`           | Directories to exclude (comma-separated, optional)                         |
| `-D`, `--default-ignores <max\|min\|none>` | Defines defualt loads enmaignore volume. (optional. default: `minimal`)      |
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
| `-w`, `--watch-dir <dir_name>`             | Watch directories (comma-separated, optional. default: `$PWD`)             |
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
| `-g`, `--ignore-dir-regex <regex>`         | Regex to ignore directories (optional)                                     |
| `-G`, `--ignore-file-regex <regex>`        | Regex to ignore files (optional)                                           |
| `-e`, `--exclude-ext <ext>`                | File extensions to exclude (comma-separated, optional)                     |
| `-E`, `--exclude-dir <dir_name>`           | Directories to exclude (comma-separated, optional)                         |
| `-D`, `--default-ignores <max\|min\|none>` | Defines defualt loads enmaignore volume. (optional. default: `minimal`)      |
| `-n`, `--enmaignore <filename>`            | enma ignore file(s) (comma-separated, optional. default: `./.enmaignore`)  |
| `--logs <log_file_path>`                   | Log file path (optional)                                                   |
| `--pid <pid_file_path>`                    | PID file path (optional)                                                   |

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

## Author

Copyright (c) 2025 Hiroshi IKEGAMI

---

## License

This project is licensed under the [MIT License](https://github.com/magicdrive/enma/blob/main/LICENSE)
