# Security Policy

## Supported versions

Only the latest release receives security fixes.

## Reporting a vulnerability

Please do not describe a vulnerability in a public issue. Report it privately through
[GitHub Security Advisories](https://github.com/magicdrive/enma/security/advisories/new)
with the enma version, your OS, and the steps to reproduce. If that form is not available,
open an issue that only asks for a private contact, without any details. You should get a
first reply within a week.

## What enma does and does not protect

enma is a local automation tool. It runs the commands from its command line or config file
with `sh -c`, as the user that started it, so:

- A config file is code. Run `enma` only on config files you trust; a plain `enma` reads
  `Enma.toml` (or `.enma.toml`, `.enma/enma.toml`, `.config/enma.toml`) in the current
  directory. `enma config validate` checks a file without running anything.
- Changed file paths are passed to commands as a shell argument, never pasted into the
  command text (see "Placeholder and the shell" in the README). A command that hands the path
  to another shell or interpreter as source text (`eval {}`, `sh -c '... {}'`, `ssh host '... {}'`)
  is outside that protection.
- enma does not log the commands it runs or their environment. A new `--logs` file is
  created with mode `0600` (an existing file keeps its mode, and a symlink at that path is
  followed); the `--pid` file holds only a process id. Do not point `--logs` or `--pid` into a
  directory other users can write to.
- On Windows, enma cannot stop processes a command leaves behind (no process groups).

Reports about these documented limits are still welcome if you find a way around them.
