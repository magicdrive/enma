# Bash and zsh completion for enma: source this file in either shell.
# Built from zsh/_enma and bash/enma-completion.bash; edit those, then run
#   go test ./internal/commandline -run TestCombinedCompletionScript -update

# -------- Zsh Section --------
if [[ -n ${ZSH_VERSION-} ]]; then
# zsh completion for enma. Install it as `_enma` in a directory of $fpath
# (Homebrew does), or source it after compinit.
#
# The flags and values here are the ones the parsers in
# internal/commandline/option define; internal/commandline/completion_test.go
# runs this file in zsh and fails when they differ. Keep it readable by
# `bash -n`: misc/completions/enma-completion.sh carries a copy for bash users.

_enma() {
  local curcontext="$curcontext" state state_descr line ret=1
  typeset -A opt_args

  # -A: the flags after the subcommand are the subcommand's (watch -c is not --config)
  _arguments -C -S -A '-*' \
    '(- 1 *)'{-h,--help}'[Show help message and exit]' \
    '(- 1 *)'{-v,--version}'[Show version]' \
    '(1 * -c --config)'{-c=,--config=}'[Run the given config file]:config file:_files' \
    '1:subcommand:_enma_subcommands' \
    '*:: :->args' && ret=0

  if [[ $state == args ]]; then
    curcontext="${curcontext%:*:*}:enma-$words[1]:"
    case $words[1] in
      init) _enma_init && ret=0 ;;
      hotload) _enma_hotload && ret=0 ;;
      watch) _enma_watch && ret=0 ;;
      scheduler) _enma_scheduler && ret=0 ;;
      config) _enma_config && ret=0 ;;
    esac
  fi
  return ret
}

_enma_subcommands() {
  local -a subcommands
  subcommands=(
    'init:Create plain Enma.toml and .enmaignore files'
    'hotload:Detect file changes, build and reload a daemon'
    'watch:Detect file changes and run a command'
    'scheduler:Run a command on a cron schedule'
    'config:Check a config file without running it'
  )
  _describe -t subcommands 'enma subcommand' subcommands
}

_enma_init() {
  _arguments -S -A '-*' \
    '(- *)'{-h,--help}'[Show help message and exit]' \
    '(-m --mode)'{-m=,--mode=}'[Mode of the file to create]:mode:(hotload watch scheduler enmaignore)' \
    '(-f --file-name)'{-f=,--file-name=}'[Name of the file to create]:file name:_files'
}

# _enma_dirs / _enma_files: complete the part after the last comma of a
# comma-separated value (a directory keeps its "/", so the path can go on;
# type the comma yourself). _arguments passes compadd options first.
_enma_dirs() {
  compset -P '*,'
  _path_files "$@" -/
}

_enma_files() {
  compset -P '*,'
  _files "$@"
}

# The flags hotload and watch share (watch has no signal and build-at-start).
_enma_watch_specs() {
  reply=(
    '(- *)'{-h,--help}'[Show help message and exit]'
    '--start-at-once=[Command run once at startup]:command:_command_names -e'
    '(-W --working-dir)'{-W=,--working-dir=}'[Working directory]:directory:_path_files -/'
    '(-I --placeholder)'{-I=,--placeholder=}'[Placeholder replaced with the changed file]:placeholder: '
    '(-s --args-path-style)'{-s=,--args-path-style=}'[File path style passed to the command]:style:_values -s , style dir base ext'
    '(-C --check-content-diff)'{-C=,--check-content-diff=}'[Fire only when the contents change]:switch:(on off)'
    '(-A --abs --absolute-path)'{-A=,--abs=,--absolute-path=}'[Pass absolute file paths]:switch:(on off)'
    '(-t --timeout)'{-t=,--timeout=}'[Command timeout]:time: '
    '(-l --delay)'{-l=,--delay=}'[Delay after the command]:time: '
    '(-r --retry)'{-r=,--retry=}'[Retry count]:number: '
    '(-w --watch-dir)'{-w=,--watch-dir=}'[Directories to watch (comma-separated)]:directory:_enma_dirs'
    '(-x --pattern-regex)'{-x=,--pattern-regex=}'[Regex of file paths to watch]:regex: '
    '(-i --include-ext)'{-i=,--include-ext=}'[File extensions to include (comma-separated)]:extension: '
    '(-g --ignore-file-regex)'{-g=,--ignore-file-regex=}'[Regex of files to ignore]:regex: '
    '(-G --ignore-dir-regex)'{-G=,--ignore-dir-regex=}'[Regex of directories to ignore]:regex: '
    '(-e --exclude-ext)'{-e=,--exclude-ext=}'[File extensions to exclude (comma-separated)]:extension: '
    '(-E --exclude-dir)'{-E=,--exclude-dir=}'[Directories to exclude (comma-separated)]:directory:_enma_dirs'
    '(-D --default-ignores)'{-D=,--default-ignores=}'[Default ignore volume]:volume:(max min none)'
    '(-n --enmaignore)'{-n=,--enmaignore=}'[Ignore files to use (comma-separated)]:ignore file:_enma_files'
    '--logs=[Path to the enma log file]:log file:_files'
    '--pid=[Path to the enma PID file]:PID file:_files'
  )
}

_enma_hotload() {
  local -a reply signals
  signals=(SIGTERM SIGKILL SIGHUP SIGINT)
  case $OSTYPE in
    cygwin*|msys*|win*) ;; # a Windows enma knows these four only
    *) signals+=(SIGUSR1 SIGUSR2) ;;
  esac
  _enma_watch_specs
  _arguments -S -A '-*' "$reply[@]" \
    '(-d --daemon)'{-d=,--daemon=}'[Daemon command]:command:_command_names -e' \
    '(-b --build)'{-b=,--build=}'[Command to build the daemon]:command:_command_names -e' \
    '(-S --signal)'{-S=,--signal=}'[Signal to stop the daemon]:signal:('"$signals"')' \
    '(-p --pre-build)'{-p=,--pre-build=}'[Command run before the build]:command:_command_names -e' \
    '(-P --post-build)'{-P=,--post-build=}'[Command run after the build]:command:_command_names -e' \
    '(-B --build-at-start)'{-B=,--build-at-start=}'[Build before the first start]:switch:(on off)'
}

_enma_watch() {
  local -a reply
  _enma_watch_specs
  _arguments -S -A '-*' "$reply[@]" \
    '(-c --cmd --command)'{-c=,--cmd=,--command=}'[Command run on a change]:command:_command_names -e' \
    '(-p --pre-cmd --pre-command)'{-p=,--pre-cmd=,--pre-command=}'[Command run before the command]:command:_command_names -e' \
    '(-P --post-cmd --post-command)'{-P=,--post-cmd=,--post-command=}'[Command run after the command]:command:_command_names -e'
}

_enma_scheduler() {
  _arguments -S -A '-*' \
    '(- *)'{-h,--help}'[Show help message and exit]' \
    '--cron=[6-field cron expression]:cron expression: ' \
    '--command=[Command to run]:command:_command_names -e' \
    '--name=[Job name]:name: ' \
    '--working-dir=[Working directory of the command]:directory:_path_files -/' \
    '--timezone=[Time zone of the cron expression]:time zone: ' \
    '--timeout=[Time limit of one attempt]:time: ' \
    '--retries=[Retries after a failed attempt]:number: ' \
    '--retry-delay=[Wait before a retry]:time: ' \
    '--max-concurrent=[Maximum attempts running at once]:number: ' \
    '--next=[List the next N fire times and exit]:number: ' \
    '--logs=[Path to the enma log file]:log file:_files' \
    '--pid=[Path to the enma PID file]:PID file:_files'
}

_enma_config() {
  local curcontext="$curcontext" state state_descr line ret=1
  _arguments -C -A '-*' \
    '(- 1 *)'{-h,--help}'[Show help message and exit]' \
    '1:action:((validate\:"Check a config file without running it"))' \
    '*:: :->args' && ret=0
  if [[ $state == args && $words[1] == validate ]]; then
    _arguments -S -A '-*' \
      '(- *)'{-h,--help}'[Show help message and exit]' \
      '(-c -f --config)'{-c=,-f=,--config=}'[Config file to check]:config file:_files' \
      '--next=[Scheduler configs: list the next N fire times]:number: ' && ret=0
  fi
  return ret
}

if [[ "${funcstack[1]-}" == _enma ]]; then
  _enma "$@" # autoloaded by compinit: complete now
else
  compdef _enma enma # sourced
fi
  return 0
fi

# -------- Bash Section --------
# bash completion for enma. Source it from ~/.bashrc, or install it in a
# bash-completion directory (Homebrew does). bash-completion is not required.
#
# The flags and values here are the ones the parsers in
# internal/commandline/option define; internal/commandline/completion_test.go
# runs this file in bash and fails when they differ.

# _enma_bash_table sets table to the "flag=kind" words of a command. A kind
# says what the flag's value is: bool (none), text (free text), command, file,
# dir, files / dirs (comma-separated), or the name of a value list.
_enma_bash_table() {
  local watch="-h=bool --help=bool --start-at-once=command -W=dir --working-dir=dir
    -I=text --placeholder=text -s=style --args-path-style=style
    -C=switch --check-content-diff=switch -A=switch --abs=switch --absolute-path=switch
    -t=text --timeout=text -l=text --delay=text -r=text --retry=text
    -w=dirs --watch-dir=dirs -x=text --pattern-regex=text -i=text --include-ext=text
    -g=text --ignore-file-regex=text -G=text --ignore-dir-regex=text
    -e=text --exclude-ext=text -E=dirs --exclude-dir=dirs
    -D=ignores --default-ignores=ignores -n=files --enmaignore=files --logs=file --pid=file"
  case $1 in
    general) table="-c=file --config=file -h=bool --help=bool -v=bool --version=bool" ;;
    init) table="-h=bool --help=bool -m=mode --mode=mode -f=file --file-name=file" ;;
    hotload) table="$watch -d=command --daemon=command -b=command --build=command
      -S=signal --signal=signal -p=command --pre-build=command -P=command --post-build=command
      -B=switch --build-at-start=switch" ;;
    watch) table="$watch -c=command --cmd=command --command=command
      -p=command --pre-cmd=command --pre-command=command
      -P=command --post-cmd=command --post-command=command" ;;
    scheduler) table="-h=bool --help=bool --cron=text --command=command --name=text
      --working-dir=dir --timezone=text --timeout=text --retries=text --retry-delay=text
      --max-concurrent=text --next=text --logs=file --pid=file" ;;
    validate) table="-h=bool --help=bool -c=file -f=file --config=file --next=text" ;;
  esac
}

# _enma_bash_add PREFIX adds the lines of its input to COMPREPLY, each after
# PREFIX (lines, not words: a file name may contain spaces). The input is a
# here-string, not < <(...): bash 3.2 in POSIX mode cannot parse that.
_enma_bash_add() {
  local line
  while IFS= read -r line; do
    [[ -n $line ]] && COMPREPLY+=("$1$line")
  done
}

# _enma_bash_dequote WORD sets word to WORD without its shell quoting
# (backslashes, '...' and "..."). Nothing in it is expanded or run.
_enma_bash_dequote() {
  local s=$1 c q= i
  word=
  for ((i = 0; i < ${#s}; i++)); do
    c=${s:i:1}
    if [[ $q == "'" ]]; then
      if [[ $c == "'" ]]; then q=; else word+=$c; fi
    elif [[ $c == "\\" ]]; then
      i=$((i + 1))
      if [[ $q == '"' ]]; then
        case ${s:i:1} in
          '"' | '\' | '$' | '`') ;;
          *) word+=$c ;; # in "...", a backslash stays before other characters
        esac
      fi
      word+=${s:i:1}
    elif [[ $c == '"' ]]; then
      if [[ $q == '"' ]]; then q=; else q='"'; fi
    elif [[ $c == "'" && -z $q ]]; then
      q="'"
    else
      word+=$c
    fi
  done
}

# _enma_bash_value KIND PREFIX VALUE adds the words that complete the value
# VALUE of a flag of kind KIND, each after PREFIX ("--flag=" for a value in
# the flag's word). With READLINE_PATH set, readline completes a path itself.
_enma_bash_value() {
  local kind=$1 prefix=$2 cur=$3 words= list= part c
  case $kind in
    signal)
      words="SIGTERM SIGKILL SIGHUP SIGINT"
      case ${OSTYPE-} in
        cygwin* | msys* | win*) ;; # a Windows enma knows these four only
        *) words="$words SIGUSR1 SIGUSR2" ;;
      esac ;;
    switch) words="on off" ;;
    ignores) words="max min none" ;;
    mode) words="hotload watch scheduler enmaignore" ;;
    style) # comma-separated, each part once
      if [[ $cur == *,* ]]; then
        list=${cur%,*},
        cur=${cur##*,}
      fi
      for part in dir base ext; do
        [[ ,$list == *,$part,* ]] || words="$words $part"
      done
      prefix=$prefix$list ;;
    command)
      _enma_bash_add "$prefix" <<<"$(compgen -c -- "$cur")"
      return 0 ;;
    file | dir | files | dirs)
      # bash 3.2 has no compopt: "complete -o default" below lets readline
      # complete a path there (files and directories)
      type compopt >/dev/null 2>&1 || return 0
      if [[ -n ${READLINE_PATH-} && ($kind != *s || $cur != *,*) ]]; then
        # readline completes the path itself: it handles quotes, spaces and
        # other special characters as for any command
        if [[ $kind == dir* ]]; then compopt -o dirnames; else compopt -o default; fi
        return 0
      fi
      if [[ $kind == *s && $cur == *,* ]]; then # the part after the last comma
        prefix=$prefix${cur%,*},
        cur=${cur##*,}
      fi
      while IFS= read -r c; do
        [[ -n $c ]] || continue
        [[ -d $c ]] && c=$c/ # the path can go on
        COMPREPLY+=("$prefix$c")
      done <<<"$(if [[ $kind == dir* ]]; then compgen -d -- "$cur"; else compgen -f -- "$cur"; fi)"
      compopt -o filenames -o nospace # readline quotes the names
      return 0 ;;
    *) return 0 ;; # free text: nothing to offer
  esac
  _enma_bash_add "$prefix" <<<"$(compgen -W "$words" -- "$cur")"
}

_enma_bash() {
  local -a raw=() words=()
  local word i w rest start end=-1 pos=0 n cur tail pre= table= cmd first kind expect= flags= p
  COMPREPLY=()

  # The shell words up to the cursor. bash splits a word at the characters of
  # COMP_WORDBREAKS ("-x a=b" is -x a = b, "-W C:/x" is -W C : /x): put the
  # pieces of one word (no space between them) back together.
  for ((i = 0; i <= COMP_CWORD; i++)); do
    w=${COMP_WORDS[i]-}
    rest=${COMP_LINE:pos}
    if [[ -n $w && $rest == *"$w"* ]]; then
      rest=${rest%%"$w"*}
      start=$((pos + ${#rest}))
    else
      start=$COMP_POINT
    fi
    # the word at the cursor counts up to the cursor (readline replaces that)
    [[ $i -eq $COMP_CWORD && $start -le $COMP_POINT ]] && w=${w:0:COMP_POINT-start}
    if [[ $start -eq $end ]]; then
      raw[${#raw[@]} - 1]+=$w
    else
      raw+=("$w")
    fi
    end=$((start + ${#w}))
    pos=$end
  done
  for w in "${raw[@]}"; do
    _enma_bash_dequote "$w"
    words+=("$word")
  done
  n=${#words[@]}
  cur=${words[n - 1]}

  # readline replaces the end of the word: after its last "=" or ":" outside
  # quotes (when COMP_WORDBREAKS has them), or after a quote left open. pre is
  # the part before it, unquoted; it is left out of each candidate.
  local r=${raw[n - 1]} c q= at=0 cut=0
  for ((i = 0; i < ${#r}; i++)); do
    c=${r:i:1}
    if [[ $q == "'" ]]; then
      [[ $c == "'" ]] && q=
    elif [[ $c == "\\" ]]; then
      i=$((i + 1))
    elif [[ $q == '"' ]]; then
      [[ $c == '"' ]] && q=
    elif [[ $c == "'" || $c == '"' ]]; then
      q=$c at=$i
    elif [[ ($c == = || $c == :) && $COMP_WORDBREAKS == *"$c"* ]]; then
      cut=$((i + 1))
    fi
  done
  [[ -n $q ]] && cut=$((at + 1))
  _enma_bash_dequote "${r:0:cut}"
  pre=$word
  if [[ -n $q ]]; then
    _enma_bash_dequote "${r:at}" # with the open quote: a quote in it is quoted
  else
    _enma_bash_dequote "${r:cut}"
  fi
  tail=$word

  _enma_bash_complete

  if [[ -n $pre && ${#COMPREPLY[@]} -gt 0 ]]; then
    local -a whole=("${COMPREPLY[@]}")
    COMPREPLY=()
    for w in "${whole[@]}"; do
      [[ $w == "$pre"* ]] && COMPREPLY+=("${w:${#pre}}")
    done
  fi
  return 0
}

# _enma_bash_complete adds the words that complete cur, the last of words.
_enma_bash_complete() {
  if [[ $n -eq 2 && $cur != -* ]]; then
    _enma_bash_add "" <<<"$(compgen -W "init hotload watch scheduler config" -- "$cur")"
    return 0
  fi
  case ${words[1]} in
    init | hotload | watch | scheduler) cmd=${words[1]} first=2 ;;
    config)
      if [[ $n -eq 3 ]]; then
        if [[ $cur == -* ]]; then
          _enma_bash_add "" <<<"$(compgen -W "-h --help" -- "$cur")"
        else
          _enma_bash_add "" <<<"$(compgen -W "validate" -- "$cur")"
        fi
        return 0
      fi
      [[ ${words[2]} == validate ]] || return 0
      cmd=validate first=3 ;;
    -*) cmd=general first=1 ;;
    *) return 0 ;; # not a subcommand of enma
  esac
  _enma_bash_table "$cmd"
  table=" ${table//$'\n'/ } "

  # Walk the words before the cursor as Go's flag package parses them: a flag
  # that takes a value consumes the next word (whatever it is), and "--" or the
  # first argument that is not a flag ends the flags.
  for ((i = first; i < n - 1; i++)); do
    w=${words[i]}
    if [[ -n $expect ]]; then
      expect=
      continue
    fi
    case $w in
      --) return 0 ;;
      -*=*) ;; # flag and value in one word
      -*)
        case $table in
          *" $w=bool "*) ;;
          *" $w="*) expect=$w ;;
        esac ;;
      *) return 0 ;; # enma ignores (or rejects) what follows an argument
    esac
  done

  local READLINE_PATH=
  if [[ -n $expect ]]; then # the cursor is on the value of a flag
    kind=${table#* $expect=}
    [[ $tail == "$cur" ]] && READLINE_PATH=1
    _enma_bash_value "${kind%% *}" "" "$cur"
    return 0
  fi
  if [[ $cur == -*=* ]]; then # --flag=value in one word
    case $table in
      *" ${cur%%=*}="*)
        kind=${table#* ${cur%%=*}=}
        [[ $tail == "${cur#*=}" ]] && READLINE_PATH=1
        _enma_bash_value "${kind%% *}" "${cur%%=*}=" "${cur#*=}"
        ;;
    esac
    return 0
  fi
  if [[ $cur == -* ]]; then
    for p in $table; do
      flags="$flags ${p%%=*}"
    done
    _enma_bash_add "" <<<"$(compgen -W "$flags" -- "$cur")"
  fi
  return 0 # no subcommand takes arguments
}

if type compopt >/dev/null 2>&1; then
  complete -F _enma_bash enma
else
  complete -o default -F _enma_bash enma # bash 3.2: paths come from readline
fi
