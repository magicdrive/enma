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
