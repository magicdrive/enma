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
