# Bash and Zsh completion script for enma
# Source this in your shell to activate

# -------- Zsh Section --------
if [[ -n ${ZSH_VERSION-} ]]; then
  #compdef enma

  _enma() {
    local context state state_descr line
    typeset -A opt_args

    local -a subcommands
    subcommands=('init:Initialize configuration' 'hotload:Watch & reload daemon' 'watch:Watch & run commands')

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
              '-h[Show help]' \
              '--mode[Specify mode <hotload|watch>]:mode:(hotload watch)' \
              '--file[Specify output config file]:file:_files'
            ;;
          hotload)
            _values 'hotload options' \
              '--daemon[Daemon command]:cmd:_command_names' \
              '--build[Build command]:cmd:_command_names' \
              '--watch-dir[Directories to watch]:dir:_files -/' \
              '--pre-build[Pre-build command]:cmd:_command_names' \
              '--post-build[Post-build command]:cmd:_command_names' \
              '--working-dir[Working directory]:dir:_files -/' \
              '--placeholder[Placeholder token]:token:' \
              '--args-path-style[Filepath style]:style:(dir base ext full)' \
              '--build-at-start[Build at startup]:bool:(on off)' \
              '--check-content-diff[Only on content change]:bool:(on off)' \
              '--absolute-path[Use absolute path]:bool:(on off)' \
              '--timeout[Timeout duration]:string:' \
              '--delay[Delay after build]:string:' \
              '--retry[Retry count]:int:' \
              '--pattern-regex[Regex to match files]:regex:' \
              '--include-ext[File extensions to include]' \
              '--exclude-ext[File extensions to exclude]' \
              '--exclude-dir[Dirs to exclude]:dir:_files -/' \
              '--ignore-dir-regex[Dir ignore regex]' \
              '--ignore-file-regex[File ignore regex]' \
              '--enmaignore[Ignore file list]:file:_files' \
              '--logs[Log file]:file:_files' \
              '--pid[PID file]:file:_files'
            ;;
          watch)
            _values 'watch options' \
              '--command[Command to run]:cmd:_command_names' \
              '--watch-dir[Directories to watch]:dir:_files -/' \
              '--pre-cmd[Pre-command]:cmd:_command_names' \
              '--post-cmd[Post-command]:cmd:_command_names' \
              '--working-dir[Working directory]:dir:_files -/' \
              '--placeholder[Placeholder token]:token:' \
              '--args-path-style[Filepath style]:style:(dir base ext full)' \
              '--check-content-diff[Only on content change]:bool:(on off)' \
              '--absolute-path[Use absolute path]:bool:(on off)' \
              '--timeout[Timeout duration]:string:' \
              '--delay[Delay duration]:string:' \
              '--retry[Retry count]:int:' \
              '--pattern-regex[Regex to match files]:regex:' \
              '--include-ext[File extensions to include]' \
              '--exclude-ext[File extensions to exclude]' \
              '--exclude-dir[Dirs to exclude]:dir:_files -/' \
              '--ignore-dir-regex[Dir ignore regex]' \
              '--ignore-file-regex[File ignore regex]' \
              '--enmaignore[Ignore file list]:file:_files' \
              '--logs[Log file]:file:_files' \
              '--pid[PID file]:file:_files'
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

  local subcommands="init hotload watch"
  local global_opts="--help -h --version -v --config -c"
  local opts

  if [[ ${COMP_CWORD} -eq 1 ]]; then
    COMPREPLY=( $(compgen -W "${subcommands} ${global_opts}" -- "$cur") )
    return 0
  fi

  local subcmd=${COMP_WORDS[1]}
  case "$subcmd" in
    init)
      opts="--help -h --mode --file"
      ;;
    hotload)
      opts="--daemon --build --watch-dir --pre-build --post-build --working-dir --placeholder --args-path-style --build-at-start --check-content-diff --absolute-path --timeout --delay --retry --pattern-regex --include-ext --exclude-ext --exclude-dir --ignore-dir-regex --ignore-file-regex --enmaignore --logs --pid"
      ;;
    watch)
      opts="--command --watch-dir --pre-cmd --post-cmd --working-dir --placeholder --args-path-style --check-content-diff --absolute-path --timeout --delay --retry --pattern-regex --include-ext --exclude-ext --exclude-dir --ignore-dir-regex --ignore-file-regex --enmaignore --logs --pid"
      ;;
    *)
      opts=$global_opts
      ;;
  esac

  COMPREPLY=( $(compgen -W "${opts}" -- "$cur") )
}

complete -F _enma_bash enma
