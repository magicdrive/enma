# Bash/Zsh shared completion for `enma`

# --------- Zsh Section ---------
if [[ -n "${ZSH_VERSION-}" ]]; then

  #compdef enma

  _enma() {
    local -a subcommands
    subcommands=(
      'init:Create config and ignore file'
      'hotload:Watch and hot-reload the daemon'
      'watch:Watch and execute commands'
    )

    _arguments -C \
      '1:Subcommand:((init\:Create\ config\ and\ ignore\ file hotload\:Watch\ and\ hot-reload\ the\ daemon watch\:Watch\ and\ execute\ commands))' \
      '*::args:->args'

    case $words[2] in
      init)
        _arguments \
          '-h[Show help]' \
          '--help[Show help]' \
          '-m[Mode <hotload|watch>]:mode:(hotload watch)' \
          '--mode[Mode <hotload|watch>]:mode:(hotload watch)' \
          '-f[Output file]:file:_files' \
          '--file[Output file]:file:_files'
        ;;
      hotload)
        _arguments \
          '*::options:->hotloadopts'
        case $state in
          hotloadopts)
            _values 'Hotload Options' \
              '-d[Daemon command]:cmd:_command_names' \
              '--daemon[Daemon command]:cmd:_command_names' \
              '-b[Build command]:cmd:_command_names' \
              '--build[Build command]:cmd:_command_names' \
              '-w[Watch directories]:dir:_files -/' \
              '--watch-dir[Watch directories]:dir:_files -/' \
              '-p[Pre-build command]:cmd:_command_names' \
              '--pre-build[Pre-build command]:cmd:_command_names' \
              '-P[Post-build command]:cmd:_command_names' \
              '--post-build[Post-build command]:cmd:_command_names' \
              '-B[Build at start <on|off>]:bool:(on off)' \
              '--build-at-start[Build at start <on|off>]:bool:(on off)' \
              '-C[Only on content change <on|off>]:bool:(on off)' \
              '--check-content-diff[Only on content change <on|off>]:bool:(on off)' \
              '-A[Use absolute path <on|off>]:bool:(on off)' \
              '--abs[Use absolute path <on|off>]:bool:(on off)' \
              '--absolute-path[Use absolute path <on|off>]:bool:(on off)' \
              '-t[Timeout]:time:' \
              '--timeout[Timeout]:time:' \
              '-l[Delay after build]:time:' \
              '--delay[Delay after build]:time:' \
              '-r[Retry count]:number:' \
              '--retry[Retry count]:number:' \
              '-x[Pattern regex]:regex:' \
              '--pattern-regex[Pattern regex]:regex:' \
              '-i[Include extensions]:exts:' \
              '--include-ext[Include extensions]:exts:' \
              '-e[Exclude extensions]:exts:' \
              '--exclude-ext[Exclude extensions]:exts:' \
              '-E[Exclude directories]:dir:_files -/' \
              '--exclude-dir[Exclude directories]:dir:_files -/' \
              '-g[Ignore dir regex]:regex:' \
              '--ignore-dir-regex[Ignore dir regex]:regex:' \
              '-G[Ignore file regex]:regex:' \
              '--ignore-file-regex[Ignore file regex]:regex:' \
              '-n[Ignore file list]:file:_files' \
              '--enmaignore[Ignore file list]:file:_files' \
              '--logs[Log file]:file:_files' \
              '--pid[PID file]:file:_files'
            ;;
        esac
        ;;
      watch)
        _arguments \
          '*::options:->watchopts'
        case $state in
          watchopts)
            _values 'Watch Options' \
              '-c[Command to run]:cmd:_command_names' \
              '--command[Command to run]:cmd:_command_names' \
              '--cmd[Command to run]:cmd:_command_names' \
              '-w[Watch directories]:dir:_files -/' \
              '--watch-dir[Watch directories]:dir:_files -/' \
              '-p[Pre command]:cmd:_command_names' \
              '--pre-cmd[Pre command]:cmd:_command_names' \
              '-P[Post command]:cmd:_command_names' \
              '--post-cmd[Post command]:cmd:_command_names' \
              '-W[Working directory]:dir:_files -/' \
              '--working-dir[Working directory]:dir:_files -/' \
              '-C[Only on content change <on|off>]:bool:(on off)' \
              '--check-content-diff[Only on content change <on|off>]:bool:(on off)' \
              '-A[Use absolute path <on|off>]:bool:(on off)' \
              '--abs[Use absolute path <on|off>]:bool:(on off)' \
              '--absolute-path[Use absolute path <on|off>]:bool:(on off)' \
              '-t[Timeout]:time:' \
              '--timeout[Timeout]:time:' \
              '-l[Delay]:time:' \
              '--delay[Delay]:time:' \
              '-r[Retry count]:number:' \
              '--retry[Retry count]:number:' \
              '-x[Pattern regex]:regex:' \
              '--pattern-regex[Pattern regex]:regex:' \
              '-i[Include extensions]:exts:' \
              '--include-ext[Include extensions]:exts:' \
              '-e[Exclude extensions]:exts:' \
              '--exclude-ext[Exclude extensions]:exts:' \
              '-E[Exclude directories]:dir:_files -/' \
              '--exclude-dir[Exclude directories]:dir:_files -/' \
              '-g[Ignore dir regex]:regex:' \
              '--ignore-dir-regex[Ignore dir regex]:regex:' \
              '-G[Ignore file regex]:regex:' \
              '--ignore-file-regex[Ignore file regex]:regex:' \
              '-n[Ignore file list]:file:_files' \
              '--enmaignore[Ignore file list]:file:_files' \
              '--logs[Log file]:file:_files' \
              '--pid[PID file]:file:_files'
            ;;
        esac
        ;;
    esac
  }

  compdef _enma enma
  return  # Prevents Bash section from executing
fi

# --------- Bash Section ---------
_enma_bash() {
  local cur prev opts subcmd
  COMPREPLY=()
  cur="${COMP_WORDS[COMP_CWORD]}"
  prev="${COMP_WORDS[COMP_CWORD-1]}"

  local subcommands="init hotload watch"
  local global_opts="--help -h --version -v --config -c"

  if [[ $COMP_CWORD -eq 1 ]]; then
    COMPREPLY=( $(compgen -W "${subcommands} ${global_opts}" -- "$cur") )
    return 0
  fi

  for word in "${COMP_WORDS[@]}"; do
    if [[ " ${subcommands} " == *" $word "* ]]; then
      subcmd=$word
      break
    fi
  done

  case "$subcmd" in
    init)
      opts="--help -h --mode -m --file -f"
      ;;
    hotload)
      opts="--daemon -d --build -b --watch-dir -w --pre-build -p --post-build -P \
--working-dir -W --placeholder -I --args-path-style -s --build-at-start -B --check-content-diff -C \
--absolute-path --abs -A --timeout -t --delay -l --retry -r --pattern-regex -x \
--include-ext -i --exclude-ext -e --exclude-dir -E --ignore-dir-regex -g --ignore-file-regex -G \
--enmaignore -n --logs --pid"
      ;;
    watch)
      opts="--command --cmd -c --watch-dir -w --pre-cmd -p --post-cmd -P \
--working-dir -W --placeholder -I --args-path-style -s --check-content-diff -C \
--absolute-path --abs -A --timeout -t --delay -l --retry -r --pattern-regex -x \
--include-ext -i --exclude-ext -e --exclude-dir -E --ignore-dir-regex -g --ignore-file-regex -G \
--enmaignore -n --logs --pid"
      ;;
    *)
      opts=$global_opts
      ;;
  esac

  COMPREPLY=( $(compgen -W "${opts}" -- "$cur") )
}

complete -F _enma_bash enma

