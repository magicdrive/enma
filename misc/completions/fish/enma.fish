# fish completion for enma. Install it as enma.fish in a directory of
# $fish_complete_path, e.g. ~/.config/fish/completions/ (Homebrew installs it
# in vendor_completions.d).
#
# The flags and values here are the ones the parsers in
# internal/commandline/option define; internal/commandline/completion_test.go
# runs this file in fish and fails when they differ.

# The words before the cursor, without "enma" (quotes removed).
function __enma_tokens
    set -l tokens (commandline -xpc 2>/dev/null; or commandline -opc)
    set -q tokens[2]
    and printf '%s\n' $tokens[2..-1]
end

# __enma_using CMD: the cursor is where CMD takes flags. CMD is general (enma
# itself), subcommand (where the subcommand goes), config (its action),
# validate (config validate) or a subcommand.
# Like Go's flag package, a flag that takes a value consumes the next word,
# and "--" or the first word that is not a flag ends the flags.
function __enma_using --argument-names cmd
    set -l tokens (__enma_tokens)
    set -l start 2
    switch $cmd
        case subcommand
            not set -q tokens[1]
            return
        case general
            if set -q tokens[1]; and not string match -q -- '-*' $tokens[1]
                return 1
            end
            set start 1
        case config
            test (count $tokens) -eq 1; and test "$tokens[1]" = config
            return
        case validate
            test "$tokens[1]" = config; and test "$tokens[2]" = validate
            or return 1
            set start 3
        case '*'
            test "$tokens[1]" = $cmd
            or return 1
    end
    set -l values __enma_values_$cmd
    set -l expect 0
    for t in $tokens[$start..-1]
        if test $expect -eq 1
            set expect 0
            continue
        end
        switch $t
            case --
                return 1
            case '-*=*' # flag and value in one word
            case '-*'
                contains -- $t $$values
                and set expect 1
            case '*'
                return 1 # enma ignores (or rejects) what follows an argument
        end
    end
    # the cursor is on the value of a flag: "-..." is that value, not a flag
    if test $expect -eq 1; and string match -q -- '-*' (commandline -ct)
        return 1
    end
    return 0
end

function __enma_signals
    printf '%s\n' SIGTERM SIGKILL SIGHUP SIGINT
    switch (uname -s 2>/dev/null)
        case 'CYGWIN*' 'MINGW*' 'MSYS*' # a Windows enma knows these four only
        case '*'
            printf '%s\n' SIGUSR1 SIGUSR2
    end
end

# The value typed so far in the current word: without quotes, and without the
# "-flag=" it may start with (fish puts that back itself).
function __enma_value
    string unescape -- (commandline -ct) | string replace -r -- '^-[^=]*=' ''
end

# __enma_list GENERATOR: completes the part after the last comma of the
# current value with what GENERATOR prints for it.
function __enma_list --argument-names generator
    set -l value (__enma_value)
    set -l head (string match -r -- '^.*,' $value)
    set -l part (string replace -r -- '^.*,' '' $value)
    for c in ($generator $part)
        printf '%s%s\n' "$head" $c
    end
end

# fish's __fish_complete_directories splits its argument at spaces: quote it
function __enma_dirs
    __fish_complete_directories (string escape -- $argv[1])
end

function __enma_styles # each part once
    set -l given (string split , -- (__enma_value))
    for s in dir base ext
        contains -- $s $given[1..-2]
        or echo $s
    end
end

# __enma_flag CMD KIND DESCRIPTION NAME...: registers the spellings NAME
# (-x, --name) of a flag of CMD. KIND says what its value is: bool (none),
# text (free text), command, file, dir, files / dirs (comma-separated), or
# the name of a value list.
function __enma_flag --argument-names cmd kind description
    set -l spec -c enma -n "__enma_using $cmd" -d $description
    for name in $argv[4..-1]
        if string match -q -- '--*' $name
            set -a spec -l (string sub -s 3 -- $name)
        else # old style: Go's flag package takes -x value and -x=value, not -xvalue
            set -a spec -o (string sub -s 2 -- $name)
        end
        test $kind = bool
        or set -ga __enma_values_$cmd $name
    end
    switch $kind
        case bool
        case text
            set -a spec -x
        case command
            set -a spec -x -a '(__fish_complete_command)'
        case file
            set -a spec -r -F
        case files
            set -a spec -x -a '(__enma_list __fish_complete_path)'
        case dir
            set -a spec -x -a '(__fish_complete_directories)'
        case dirs
            set -a spec -x -a '(__enma_list __enma_dirs)'
        case signal
            set -a spec -x -a '(__enma_signals)'
        case switch
            set -a spec -x -a 'on off'
        case ignores
            set -a spec -x -a 'max min none'
        case mode
            set -a spec -x -a 'hotload watch scheduler enmaignore'
        case style
            set -a spec -x -a '(__enma_list __enma_styles)'
    end
    complete $spec
end

complete -c enma -e # loading the file again starts over
set -l __enma_old (set -n | string match '__enma_values_*')
set -q __enma_old[1]; and set -e $__enma_old
complete -c enma -f

complete -c enma -n '__enma_using subcommand' -a init -d 'Create plain Enma.toml and .enmaignore files'
complete -c enma -n '__enma_using subcommand' -a hotload -d 'Detect file changes, build and reload a daemon'
complete -c enma -n '__enma_using subcommand' -a watch -d 'Detect file changes and run a command'
complete -c enma -n '__enma_using subcommand' -a scheduler -d 'Run a command on a cron schedule'
complete -c enma -n '__enma_using subcommand' -a config -d 'Check a config file without running it'
complete -c enma -n '__enma_using config' -a validate -d 'Check a config file without running it'

__enma_flag general bool 'Show help message and exit' -h --help
__enma_flag general bool 'Show version' -v --version
__enma_flag general file 'Run the given config file' -c --config

__enma_flag config bool 'Show help message and exit' -h --help

__enma_flag validate bool 'Show help message and exit' -h --help
__enma_flag validate file 'Config file to check' -c -f --config
__enma_flag validate text 'Scheduler configs: list the next N fire times' --next

__enma_flag init bool 'Show help message and exit' -h --help
__enma_flag init mode 'Mode of the file to create' -m --mode
__enma_flag init file 'Name of the file to create' -f --file-name

# the flags hotload and watch share (watch has no signal and build-at-start)
for __enma_cmd in hotload watch
    __enma_flag $__enma_cmd bool 'Show help message and exit' -h --help
    __enma_flag $__enma_cmd command 'Command run once at startup' --start-at-once
    __enma_flag $__enma_cmd dir 'Working directory' -W --working-dir
    __enma_flag $__enma_cmd text 'Placeholder replaced with the changed file' -I --placeholder
    __enma_flag $__enma_cmd style 'File path style passed to the command' -s --args-path-style
    __enma_flag $__enma_cmd switch 'Fire only when the contents change' -C --check-content-diff
    __enma_flag $__enma_cmd switch 'Pass absolute file paths' -A --abs --absolute-path
    __enma_flag $__enma_cmd text 'Command timeout' -t --timeout
    __enma_flag $__enma_cmd text 'Delay after the command' -l --delay
    __enma_flag $__enma_cmd text 'Retry count' -r --retry
    __enma_flag $__enma_cmd dirs 'Directories to watch (comma-separated)' -w --watch-dir
    __enma_flag $__enma_cmd text 'Regex of file paths to watch' -x --pattern-regex
    __enma_flag $__enma_cmd text 'File extensions to include (comma-separated)' -i --include-ext
    __enma_flag $__enma_cmd text 'Regex of files to ignore' -g --ignore-file-regex
    __enma_flag $__enma_cmd text 'Regex of directories to ignore' -G --ignore-dir-regex
    __enma_flag $__enma_cmd text 'File extensions to exclude (comma-separated)' -e --exclude-ext
    __enma_flag $__enma_cmd dirs 'Directories to exclude (comma-separated)' -E --exclude-dir
    __enma_flag $__enma_cmd ignores 'Default ignore volume' -D --default-ignores
    __enma_flag $__enma_cmd files 'Ignore files to use (comma-separated)' -n --enmaignore
    __enma_flag $__enma_cmd file 'Path to the enma log file' --logs
    __enma_flag $__enma_cmd file 'Path to the enma PID file' --pid
end

__enma_flag hotload command 'Daemon command' -d --daemon
__enma_flag hotload command 'Command to build the daemon' -b --build
__enma_flag hotload signal 'Signal to stop the daemon' -S --signal
__enma_flag hotload command 'Command run before the build' -p --pre-build
__enma_flag hotload command 'Command run after the build' -P --post-build
__enma_flag hotload switch 'Build before the first start' -B --build-at-start

__enma_flag watch command 'Command run on a change' -c --cmd --command
__enma_flag watch command 'Command run before the command' -p --pre-cmd --pre-command
__enma_flag watch command 'Command run after the command' -P --post-cmd --post-command

__enma_flag scheduler bool 'Show help message and exit' -h --help
__enma_flag scheduler text '6-field cron expression' --cron
__enma_flag scheduler command 'Command to run' --command
__enma_flag scheduler text 'Job name' --name
__enma_flag scheduler dir 'Working directory of the command' --working-dir
__enma_flag scheduler text 'Time zone of the cron expression' --timezone
__enma_flag scheduler text 'Time limit of one attempt' --timeout
__enma_flag scheduler text 'Retries after a failed attempt' --retries
__enma_flag scheduler text 'Wait before a retry' --retry-delay
__enma_flag scheduler text 'Maximum attempts running at once' --max-concurrent
__enma_flag scheduler text 'List the next N fire times and exit' --next
__enma_flag scheduler file 'Path to the enma log file' --logs
__enma_flag scheduler file 'Path to the enma PID file' --pid

functions -e __enma_flag
set -e __enma_cmd
