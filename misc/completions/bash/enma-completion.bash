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
      opts="--daemon --build --signal --watch-dir --pre-build --post-build --working-dir --placeholder --args-path-style --build-at-start --check-content-diff --absolute-path --timeout --delay --retry --pattern-regex --include-ext --exclude-ext --exclude-dir --ignore-dir-regex --ignore-file-regex --default-ignores --enmaignore --logs --pid"
      ;;
    watch)
      opts="--command --watch-dir --pre-cmd --post-cmd --working-dir --placeholder --args-path-style --check-content-diff --absolute-path --timeout --delay --retry --pattern-regex --include-ext --exclude-ext --exclude-dir --ignore-dir-regex --ignore-file-regex --default-ignores --enmaignore --logs --pid"
      ;;
    *)
      opts=$global_opts
      ;;
  esac

  COMPREPLY=( $(compgen -W "${opts}" -- "$cur") )
}

complete -F _enma_bash enma
