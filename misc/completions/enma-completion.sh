# Bash and Zsh completion script for enma
# Source this in your shell to activate

# -------- Zsh Section --------
if [[ -n ${ZSH_VERSION-} ]]; then
  #compdef enma

  _enma() {
    local context state state_descr line
    typeset -A opt_args

    local -a subcommands
    subcommands=(
        'init:Initialize configuration'
        'hotload:Watch & reload daemon'
        'watch:Watch & run commands'
        'scheduler:Run commands on cron schedules'
        'config:Check a config file without running it (validate)'
        '--help:Show help'
        '--version:Show version'
        '--config:Define enma.toml'
    )

    _arguments -C \
      '1:command:->subcmd' \
      '*::options:->args'

    case $state in
      subcmd)
        _describe 'subcommand' subcommands
        ;;
      args)
        case $words[1] in
          init)
            _values 'init options' \
              '--help[Show help]' \
              '--mode[Specify mode <hotload|watch|scheduler|enmaignore>]:mode:(hotload watch scheduler enmaignore)' \
              '--file-name[Specify output config file]:file:_files'
            ;;
          hotload)
            if case $OSTYPE in linux*|darwin*|freebsd*|openbsd*|netbsd*) true ;; *) false ;; esac; then # bash parses this file too
                _values 'hotload options' \
                  '--help[Show help]' \
                  '--daemon[Daemon command]:cmd:_command_names' \
                  '--build[Build command]:cmd:_command_names' \
                  '--start-at-once[    (optional) Command run once at startup]:cmd:_command_names' \
                  '--signal[    (optional) signal to stop daemon command]:sig:( SIGTERM SIGKILL SIGHUP SIGUSR1 SIGUSR2 SIGINT )' \
                  '--watch-dir[    (optional) Directories to watch]:dir:_files -/' \
                  '--pre-build[    (optional) Pre-build command]:cmd:_command_names' \
                  '--post-build[    (optional) Post-build command]:cmd:_command_names' \
                  '--working-dir[    (optional) Working directory]:dir:_files -/' \
                  '--placeholder[    (optional) Placeholder token]:token:' \
                  '--args-path-style[    (optional) Filepath style]:style:(dir base ext)' \
                  '--build-at-start[    (optional) Build at startup]:bool:(on off)' \
                  '--check-content-diff[    (optional) Only on content change]:bool:(on off)' \
                  '--absolute-path[    (optional) Use absolute path]:bool:(on off)' \
                  '--timeout[    (optional) Timeout duration]:string:' \
                  '--delay[    (optional) Delay after build]:string:' \
                  '--retry[    (optional) Retry count]:int:' \
                  '--pattern-regex[    (optional) Regex to match files]:regex:' \
                  '--include-ext[    (optional) File extensions to include]' \
                  '--exclude-ext[    (optional) File extensions to exclude]' \
                  '--exclude-dir[    (optional) Dirs to exclude]:dir:_files -/' \
                  '--ignore-dir-regex[    (optional) Dir ignore regex]' \
                  '--ignore-file-regex[    (optional) File ignore regex]' \
                  '--default-ignores[    (optional) Specify default ignore volume <max|min|none>]:mode:(maximum minimal none)' \
                  '--enmaignore[    (optional) Ignore file list]:file:_files' \
                  '--logs[    (optional) Log file]:file:_files' \
                  '--pid[    (optional) PID file]:file:_files'
            else
                _values 'hotload options' \
                  '--help[Show help]' \
                  '--daemon[Daemon command]:cmd:_command_names' \
                  '--build[Build command]:cmd:_command_names' \
                  '--start-at-once[    (optional) Command run once at startup]:cmd:_command_names' \
                  '--signal[    (optional) signal to stop daemon command]:sig:( SIGTERM SIGKILL SIGHUP SIGINT )' \
                  '--watch-dir[    (optional) Directories to watch]:dir:_files -/' \
                  '--pre-build[    (optional) Pre-build command]:cmd:_command_names' \
                  '--post-build[    (optional) Post-build command]:cmd:_command_names' \
                  '--working-dir[    (optional) Working directory]:dir:_files -/' \
                  '--placeholder[    (optional) Placeholder token]:token:' \
                  '--args-path-style[    (optional) Filepath style]:style:(dir base ext)' \
                  '--build-at-start[    (optional) Build at startup]:bool:(on off)' \
                  '--check-content-diff[    (optional) Only on content change]:bool:(on off)' \
                  '--absolute-path[    (optional) Use absolute path]:bool:(on off)' \
                  '--timeout[    (optional) Timeout duration]:string:' \
                  '--delay[    (optional) Delay after build]:string:' \
                  '--retry[    (optional) Retry count]:int:' \
                  '--pattern-regex[    (optional) Regex to match files]:regex:' \
                  '--include-ext[    (optional) File extensions to include]' \
                  '--exclude-ext[    (optional) File extensions to exclude]' \
                  '--exclude-dir[    (optional) Dirs to exclude]:dir:_files -/' \
                  '--ignore-dir-regex[    (optional) Dir ignore regex]' \
                  '--ignore-file-regex[    (optional) File ignore regex]' \
                  '--default-ignores[    (optional) Specify default ignore volume <max|min|none>]:mode:(maximum minimal none)' \
                  '--enmaignore[    (optional) Ignore file list]:file:_files' \
                  '--logs[    (optional) Log file]:file:_files' \
                  '--pid[    (optional) PID file]:file:_files'
            fi
            ;;
          watch)
            _values 'watch options' \
              '--help[Show help]' \
              '--command[Command to run]:cmd:_command_names' \
              '--start-at-once[    (optional) Command run once at startup]:cmd:_command_names' \
              '--watch-dir[    (optional) Directories to watch]:dir:_files -/' \
              '--pre-cmd[    (optional) Pre-command]:cmd:_command_names' \
              '--post-cmd[    (optional) Post-command]:cmd:_command_names' \
              '--working-dir[    (optional) Working directory]:dir:_files -/' \
              '--placeholder[    (optional) Placeholder token]:token:' \
              '--args-path-style[    (optional) Filepath style]:style:(dir base ext)' \
              '--check-content-diff[    (optional) Only on content change]:bool:(on off)' \
              '--absolute-path[    (optional) Use absolute path]:bool:(on off)' \
              '--timeout[    (optional) Timeout duration]:string:' \
              '--delay[    (optional) Delay duration]:string:' \
              '--retry[    (optional) Retry count]:int:' \
              '--pattern-regex[    (optional) Regex to match files]:regex:' \
              '--include-ext[    (optional) File extensions to include]' \
              '--exclude-ext[    (optional) File extensions to exclude]' \
              '--exclude-dir[    (optional) Dirs to exclude]:dir:_files -/' \
              '--ignore-dir-regex[    (optional) Dir ignore regex]' \
              '--ignore-file-regex[    (optional) File ignore regex]' \
              '--default-ignores[    (optional) Specify default ignore volume <max|min|none>]:mode:(maximum minimal none)' \
              '--enmaignore[    (optional) Ignore file list]:file:_files' \
              '--logs[    (optional) Log file]:file:_files' \
              '--pid[    (optional) PID file]:file:_files'
            ;;
          scheduler)
            _values 'scheduler options' \
              '--help[Show help]' \
              '--cron[6-field cron expression]:cron:' \
              '--command[Command to run]:cmd:_command_names' \
              '--name[    (optional) Job name]:name:' \
              '--working-dir[    (optional) Working directory]:dir:_files -/' \
              '--timezone[    (optional) Time zone]:zone:' \
              '--timeout[    (optional) Time limit of one attempt]:string:' \
              '--retries[    (optional) Retries after a failure]:int:' \
              '--retry-delay[    (optional) Wait before a retry]:string:' \
              '--max-concurrent[    (optional) Maximum attempts at once]:int:' \
              '--next[    (optional) List the next N fire times and exit]:int:' \
              '--logs[    (optional) Log file]:file:_files' \
              '--pid[    (optional) PID file]:file:_files'
            ;;
          config)
            _values 'config options' \
              'validate[Check a config file without running it]' \
              '--help[Show help]' \
              '--config[Config file to check]:file:_files' \
              '--next[    (optional) Scheduler configs: list the next N fire times]:int:'
            ;;
          *)
            _message 'No subcommand matched.'
            ;;
        esac
        ;;
    esac
  }

  compdef _enma enma
  return 0
fi

# -------- Bash Section --------
_enma_bash() {
  local cur prev words cword
  _init_completion -n : || return

  local subcommands="init hotload watch scheduler config"
  local global_opts="--help -h --version -v --config -c"
  local opts

  if [[ ${COMP_CWORD} -eq 1 ]]; then
    COMPREPLY=( $(compgen -W "${subcommands} ${global_opts}" -- "$cur") )
    return 0
  fi

  local subcmd=${COMP_WORDS[1]}
  case "$subcmd" in
    init)
      opts="--help -h --mode -m --file-name -f"
      ;;
    hotload)
      opts="--help -h --daemon --build --start-at-once --signal --watch-dir --pre-build --post-build --working-dir --placeholder --args-path-style --build-at-start --check-content-diff --absolute-path --timeout --delay --retry --pattern-regex --include-ext --exclude-ext --exclude-dir --ignore-dir-regex --ignore-file-regex --default-ignores --enmaignore --logs --pid"
      ;;
    watch)
      opts="--help -h --command --start-at-once --watch-dir --pre-cmd --post-cmd --working-dir --placeholder --args-path-style --check-content-diff --absolute-path --timeout --delay --retry --pattern-regex --include-ext --exclude-ext --exclude-dir --ignore-dir-regex --ignore-file-regex --default-ignores --enmaignore --logs --pid"
      ;;
    scheduler)
      opts="--cron --command --name --working-dir --timezone --timeout --retries --retry-delay --max-concurrent --next --logs --pid --help"
      ;;
    config)
      if [[ ${COMP_CWORD} -eq 2 ]]; then
        opts="validate --help -h"
      elif [[ "$prev" == "--config" || "$prev" == "-c" || "$prev" == "-f" ]]; then
        COMPREPLY=( $(compgen -f -- "$cur") )
        return 0
      else
        opts="--config -c -f --next --help -h"
      fi
      ;;
    *)
      opts=$global_opts
      ;;
  esac

  COMPREPLY=( $(compgen -W "${opts}" -- "$cur") )
}

complete -F _enma_bash enma
