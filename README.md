# enma

> Yet another integration software with file monitoring.

`enma` is a file-watching tool that monitors specified files or directories and automatically executes commands or rebuilds and restarts daemons.  
It's designed to supercharge your development and automation workflows.

## ✨ Features

- 📂 Realtime monitoring for directories or files
- 🛠️ Execute build or custom commands on file changes
- 🔁 Hot-reload support with build success detection
- 🔗 Supports symlinks
- 🧩 Flexible configuration using TOML files
- 🔍 Ignore patterns with `.enmaignore`
- 🧪 Ideal for CI/CD and local development

---

## 🚀 Quick Start

### 1. Installation

```bash
go install github.com/magicdrive/enma@latest
```

### 2. Initialize project

```bash
enma init
```

This generates `Enma.toml` and `.enmaignore`.

> You can specify the mode or config file name:
>
> ```bash
> enma init --mode watch --file ./myconfig.toml
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
enma hotload --daemon ./my-app --build "go build -o my-app main.go" --watch-dir ./cmd,./internal
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
enma watch --command "make test" --watch-dir ./pkg,./lib
```

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
| `-m`, `--mode`                | Mode for config file: `hotload` or `watch` (default: `hotload`) |
| `-f`, `--file <filename>`     | Config filename to create (default: `./Enma.toml`)        |

---

### `enma hotload`

| Option                                | Description                                                                 |
|---------------------------------------|-----------------------------------------------------------------------------|
| `-d`, `--daemon <command>`            | Daemon command to run (required)                                           |
| `-b`, `--build <command>`             | Command to build the daemon (required)                                     |
| `-w`, `--watch-dir <dir_name>`        | Watch directories (comma-separated, required)                              |
| `-p`, `--pre-build <command>`         | Command to run before build (optional)                                     |
| `-P`, `--post-build <command>`        | Command to run after build (optional)                                      |
| `-I`, `--placeholder`                 | Placeholder in command for changed file (default: `{}`)                    |
| `-A`, `--abs`, `--absolute-path`      | Use absolute path in placeholder (optional)                                |
| `-t`, `--timeout <time>`             | Timeout for build command (default: `5sec`)                                |
| `-l`, `--delay <time>`               | Delay after build command (default: `5sec`)                                |
| `-r`, `--retry <number>`             | Retry count (default: `0`)                                                 |
| `-x`, `--pattern-regex <regex>`      | Regex pattern to watch (optional)                                          |
| `-i`, `--include-ext <ext>`          | File extensions to include (comma-separated, optional)                     |
| `-g`, `--ignore-dir-regex <regex>`   | Regex to ignore directories (optional)                                     |
| `-G`, `--ignore-file-regex <regex>`  | Regex to ignore files (optional)                                           |
| `-e`, `--exclude-ext <ext>`          | File extensions to exclude (comma-separated, optional)                     |
| `-E`, `--exclude-dir <dir_name>`     | Directories to exclude (comma-separated, optional)                         |
| `-n`, `--enmaignore <filename>`      | enma ignore file(s) (comma-separated, optional. default: `./.enmaignore`)  |
| `--logs <log_file_path>`             | Log file path (optional)                                                   |
| `--pid <pid_file_path>`              | PID file path (optional)                                                   |

---

### `enma watch`

| Option                                | Description                                                                 |
|---------------------------------------|-----------------------------------------------------------------------------|
| `-c`, `--command`, `--cmd <command>`  | Command to run on file change (required)                                   |
| `-w`, `--watch-dir <dir_name>`        | Watch directories (comma-separated, required)                              |
| `-p`, `--pre-cmd <command>`           | Command to run before main command (optional)                              |
| `-P`, `--post-cmd <command>`          | Command to run after main command (optional)                               |
| `-I`, `--placeholder`                 | Placeholder in command for changed file (default: `{}`)                    |
| `-A`, `--abs`, `--absolute-path`      | Use absolute path in placeholder (optional)                                |
| `-t`, `--timeout <time>`             | Timeout for command (default: `5sec`)                                      |
| `-l`, `--delay <time>`               | Delay after command (default: `5sec`)                                      |
| `-r`, `--retry <number>`             | Retry count (default: `0`)                                                 |
| `-x`, `--pattern-regex <regex>`      | Regex pattern to watch (optional)                                          |
| `-i`, `--include-ext <ext>`          | File extensions to include (comma-separated, optional)                     |
| `-g`, `--ignore-dir-regex <regex>`   | Regex to ignore directories (optional)                                     |
| `-G`, `--ignore-file-regex <regex>`  | Regex to ignore files (optional)                                           |
| `-e`, `--exclude-ext <ext>`          | File extensions to exclude (comma-separated, optional)                     |
| `-E`, `--exclude-dir <dir_name>`     | Directories to exclude (comma-separated, optional)                         |
| `-n`, `--enmaignore <filename>`      | enma ignore file(s) (comma-separated, optional. default: `./.enmaignore`)  |
| `--logs <log_file_path>`             | Log file path (optional)                                                   |
| `--pid <pid_file_path>`              | PID file path (optional)                                                   |

---

## 🗂 Example `.enmaignore`

```
*.log
tmp/
vendor/
```

---

## 📚 Documentation

- Full documentation: [https://github.com/magicdrive/enma/README.md](https://github.com/magicdrive/enma/README.md)

---

## Author

[magicdrive](https://github.com/magicdrive)

---

## License

[MIT](https://github.com/magicdrive/enma/blob/main/LICENSE)
