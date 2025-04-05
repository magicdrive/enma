# enma

> Yet another integration software with file monitoring.

`enma` is a flexible command-line tool that monitors file system events and automatically executes defined actions. It can be used to reload daemons, run builds, or trigger custom scripts whenever your files change.

---

## 🚀 Features

- Hot reloading with build & restart for daemons
- Flexible file watching with include/exclude filters
- Command execution on file events
- TOML-based configuration
- Cross-platform support (Linux, macOS, Windows)

---

## 📦 Installation

```bash
# Install via Go
go install github.com/magicdrive/enma@latest
```

---

## 🧭 Usage

```bash
enma [SUBCOMMAND] [OPTIONS]
```

### 🔧 Subcommands

| Command   | Description                                                       |
|-----------|-------------------------------------------------------------------|
| `init`    | Create a new configuration file                                   |
| `hotload` | Watch files and reload the daemon on changes                      |
| `watch`   | Watch files and execute a command on changes                      |

### 🌐 Global Options

| Option              | Description                                                         |
|---------------------|---------------------------------------------------------------------|
| `-h`, `--help`       | Show help message and exit                                          |
| `-v`, `--version`    | Show version                                                        |
| `-c`, `--config`     | Specify config file (default: `./.enma.toml`, `./.enma/enma.toml`) |

---

## ⚙️ Subcommand: `init`

Create a configuration file for `hotload` or `watch` modes.

| Option                    | Description                                                |
|---------------------------|------------------------------------------------------------|
| `-m`, `--mode`            | Mode: `hotload` or `watch` (default: `hotload`)            |
| `-f`, `--file`            | Output filename (default: `./Enma.toml`)                  |

---

## 🔥 Subcommand: `hotload`

Watches directories and reloads a daemon after running a build process.

### Required Options

- `-n`, `--name <name>` — Process name
- `-d`, `--daemon <command>` — Daemon command
- `-b`, `--build <command>` — Build command
- `-w`, `--watch-dir <dir>` — Directories to watch (comma-separated)

### Optional Options

| Option                          | Description                                                                 |
|---------------------------------|-----------------------------------------------------------------------------|
| `-p`, `--pre-build`             | Command to run before build                                                 |
| `-P`, `--post-build`            | Command to run after build                                                  |
| `-I`, `--placeholder`           | Placeholder in commands (default: `{}`)                                     |
| `-A`, `--abs`                   | Use absolute path for placeholder                                           |
| `-t`, `--timeout <duration>`    | Timeout for build command (default: `5s`)                                   |
| `-l`, `--delay <duration>`      | Delay after successful build (default: `5s`)                                |
| `-r`, `--retry <count>`         | Number of build retries (default: `0`)                                      |
| `-x`, `--pattern-regex <regex>` | Regex pattern to match file names                                           |
| `-i`, `--include-ext <ext>`     | File extensions to include (comma-separated, e.g., `.go,.ts`)              |
| `-g`, `--ignore-dir-regex`      | Regex to ignore directory paths                                             |
| `-G`, `--ignore-file-regex`     | Regex to ignore file names                                                  |
| `-e`, `--exclude-ext <ext>`     | File extensions to exclude                                                  |
| `-E`, `--exclude-dir <dir>`     | Directory names to exclude                                                  |
| `--log-path`                    | Log file path (default: `./.enma/log/enma_<name>.log`)                      |
| `--pid-path`                    | PID file path (default: `./.enma/run/enma_<name>.pid`)                      |

---

## 👀 Subcommand: `watch`

Watches directories and executes a command when a change is detected.

### Required Options

- `-n`, `--name <name>` — Process name
- `-c`, `--command <cmd>` — Command to run
- `-w`, `--watch-dir <dir>` — Directories to watch

### Optional Options

| Option                          | Description                                                                 |
|---------------------------------|-----------------------------------------------------------------------------|
| `-p`, `--pre-cmd`               | Command to run before execution                                             |
| `-P`, `--post-cmd`              | Command to run after execution                                              |
| `-I`, `--placeholder`           | Placeholder in commands (default: `{}`)                                     |
| `-A`, `--abs`                   | Use absolute path for placeholder                                           |
| `-t`, `--timeout <duration>`    | Timeout for command (default: `5s`)                                         |
| `-l`, `--delay <duration>`      | Delay after execution (default: `5s`)                                       |
| `-r`, `--retry <count>`         | Number of retries (default: `0`)                                            |
| `-x`, `--pattern-regex <regex>` | Regex pattern to match file names                                           |
| `-i`, `--include-ext <ext>`     | File extensions to include                                                  |
| `-g`, `--ignore-dir-regex`      | Regex to ignore directory paths                                             |
| `-G`, `--ignore-file-regex`     | Regex to ignore file names                                                  |
| `-e`, `--exclude-ext <ext>`     | File extensions to exclude                                                  |
| `-E`, `--exclude-dir <dir>`     | Directory names to exclude                                                  |
| `--log-path`                    | Log file path (default: `./.enma/log/enma_<name>.log`)                      |
| `--pid-path`                    | PID file path (default: `./.enma/run/enma_<name>.pid`)                      |

---

## 📄 Configuration File

You can use a `TOML` file to persist configuration. Run `enma init` to scaffold a new file.

---

## 📚 See Also

- Documentation: [GitHub README](https://github.com/magicdrive/enma/README.md)

---

## 🧪 Example

```bash
enma hotload \
  --name myapp \
  --watch-dir ./src \
  --build "go build -o bin/app ./cmd/app" \
  --daemon "./bin/app"
```

---

## 🛠 License

MIT © [magicdrive](https://github.com/magicdrive)

