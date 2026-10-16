// Package shellcmd turns an enma command with placeholders into a shell script
// that receives the changed file's path as a positional parameter.
package shellcmd

import "strings"

// Argv0 is the $0 seen by commands run through Args.
const Argv0 = "enma"

// PathVar is the shell variable that holds the changed file's path inside
// a command run through Args. It is copied from $1 at the start of
// the script, so placeholders keep their value inside shell functions and
// after `set --` or `shift`.
const PathVar = "__enma_path"

// Args returns the arguments for `sh` that run command with every
// placeholder bound to path.
//
// The path is never spliced into the shell source: it is passed as the
// positional parameter $1 and each placeholder becomes a reference to it that
// fits its quoting context (see Script). A file name therefore cannot
// inject syntax into this shell, and it reaches the command as exactly one word.
func Args(command, placeholder, path string) []string {
	return []string{"-c", Script(command, placeholder), Argv0, path}
}

// lexer contexts of a POSIX shell command, as far as quoting is concerned.
const (
	ctxPlain          = iota // top level, or inside $( ... )
	ctxSingle                // '...'
	ctxDouble                // "..."
	ctxBacktick              // `...` outside double quotes
	ctxBacktickDouble        // `...` inside "..."
	ctxDoubleInBq            // \"...\" inside "`...`"
	ctxAnsi                  // $'...' (bash, zsh)
	ctxArith                 // $(( ... )) or (( ... ))
	ctxHeredoc               // body of an unquoted here-document
)

// states of a case ... esac construct
const (
	caseWord    = iota // after "case", before "in"
	casePattern        // in a pattern list, before ")"
	caseBody           // in the commands of an item, before ";;"
)

type lexFrame struct {
	ctx       int
	arithCmd  bool  // arith: (( ... )) in command position, which may also be a subshell
	parens    int   // open "(" (plain: inside $( ... ); arith: inside the expression)
	cases     []int // open case constructs (state of each)
	delim     string
	stripTabs bool
	lineStart bool // heredoc: at the start of a body line
}

type heredoc struct {
	delim     string
	quoted    bool
	stripTabs bool
}

type shellLexer struct {
	src, ph  string
	i        int
	b        strings.Builder
	stack    []lexFrame
	pending  []heredoc
	replaced bool
}

// Script rewrites command so that each placeholder refers to the path
// variable instead of containing the path:
//
//	outside quotes           {}  ->  "${__enma_path}"
//	inside "..."             {}  ->  ${__enma_path}
//	inside '...'             {}  ->  '"${__enma_path}"'   (close, insert, reopen)
//	inside $'...'            {}  ->  '"${__enma_path}"$'
//	in $(( ... ))            {}  ->  ${__enma_path}
//	in an unquoted here-doc  {}  ->  ${__enma_path}
//
// When a placeholder was replaced, `__enma_path="$1"; ` is put in front of the
// script. Placeholders are left as they are where the shell would not expand a
// variable: after a backslash, in comments and in quoted here-documents
// (<<'EOF'). An empty placeholder disables substitution.
func Script(command, placeholder string) string {
	l := lex(command, placeholder)
	if l == nil || !l.replaced {
		return command
	}
	return PathVar + `="$1"; ` + l.b.String()
}

// HasPlaceholder reports whether command has a placeholder that Script
// replaces, that is one the shell expands to the path. Placeholders after a
// backslash, in comments or in quoted here-documents do not count, and an empty
// placeholder never does.
func HasPlaceholder(command, placeholder string) bool {
	l := lex(command, placeholder)
	return l != nil && l.replaced
}

// lex runs the lexer over command; nil means there is nothing to replace.
func lex(command, placeholder string) *shellLexer {
	if placeholder == "" || !strings.Contains(command, placeholder) {
		return nil
	}
	l := &shellLexer{src: command, ph: placeholder, stack: []lexFrame{{ctx: ctxPlain}}}
	l.run()
	return l
}

func (l *shellLexer) top() *lexFrame  { return &l.stack[len(l.stack)-1] }
func (l *shellLexer) push(f lexFrame) { l.stack = append(l.stack, f) }
func (l *shellLexer) pop() {
	if len(l.stack) > 1 {
		l.stack = l.stack[:len(l.stack)-1]
	}
}

func (l *shellLexer) has(s string) bool { return strings.HasPrefix(l.src[l.i:], s) }

// copyN copies n bytes of the source to the output.
func (l *shellLexer) copyN(n int) {
	if l.i+n > len(l.src) {
		n = len(l.src) - l.i
	}
	l.b.WriteString(l.src[l.i : l.i+n])
	l.i += n
}

func (l *shellLexer) emit(ref string) {
	l.b.WriteString(ref)
	l.i += len(l.ph)
	l.replaced = true
}

func isShellSep(c byte) bool { return strings.IndexByte(" \t\n;&|()<>", c) >= 0 }

func (l *shellLexer) atWordStart() bool { return l.i == 0 || isShellSep(l.src[l.i-1]) }

// word reports whether the word w starts at the current position.
func (l *shellLexer) word(w string) bool {
	if !l.atWordStart() || !l.has(w) {
		return false
	}
	end := l.i + len(w)
	return end == len(l.src) || isShellSep(l.src[end])
}

// atCommandStart reports whether a reserved word would be recognised at the
// current position: at the start of the input, after an operator, or after a
// reserved word that is itself in command position.
func (l *shellLexer) atCommandStart() bool { return commandStartAt(l.src, l.i) }

// prevWord returns the word before the current position (skipping blanks).
func (l *shellLexer) prevWord() string {
	j := l.i - 1
	for j >= 0 && (l.src[j] == ' ' || l.src[j] == '\t') {
		j--
	}
	end := j + 1
	for j >= 0 && !isShellSep(l.src[j]) {
		j--
	}
	return l.src[j+1 : end]
}

func commandStartAt(src string, i int) bool {
	j := i - 1
	for j >= 0 && (src[j] == ' ' || src[j] == '\t') {
		j--
	}
	if j < 0 || strings.IndexByte(";&|(\n", src[j]) >= 0 {
		return true
	}
	// "{", "}" and "!" are reserved words only as words of their own.
	if strings.IndexByte("{}!", src[j]) >= 0 && (j == 0 || isShellSep(src[j-1])) {
		return true
	}
	end := j + 1
	for j >= 0 && !isShellSep(src[j]) {
		j--
	}
	switch src[j+1 : end] {
	case "then", "do", "else", "elif", "if", "while", "until":
		return commandStartAt(src, j+1)
	}
	return false
}

func (l *shellLexer) run() {
	for l.i < len(l.src) {
		switch l.top().ctx {
		case ctxSingle:
			l.single()
		case ctxAnsi:
			l.ansi()
		case ctxDouble:
			l.double(false)
		case ctxDoubleInBq:
			l.double(true)
		case ctxBacktickDouble:
			l.backtickInDouble()
		case ctxArith:
			l.arith()
		case ctxHeredoc:
			l.heredocBody()
		default: // ctxPlain, ctxBacktick
			l.plain()
		}
	}
}

func (l *shellLexer) single() {
	switch {
	case l.has(l.ph):
		l.emit(`'"${` + PathVar + `}"'`)
	case l.src[l.i] == '\'':
		l.pop()
		l.copyN(1)
	default:
		l.copyN(1)
	}
}

func (l *shellLexer) ansi() {
	switch {
	case l.has(l.ph):
		l.emit(`'"${` + PathVar + `}"$'`)
	case l.src[l.i] == '\\':
		l.copyN(2)
	case l.src[l.i] == '\'':
		l.pop()
		l.copyN(1)
	default:
		l.copyN(1)
	}
}

// substitution opens $( ... ) or $(( ... )) at the current position.
func (l *shellLexer) substitution() {
	if l.has("$((") {
		l.push(lexFrame{ctx: ctxArith})
		l.copyN(3)
		return
	}
	l.push(lexFrame{ctx: ctxPlain})
	l.copyN(2)
}

// double handles "..." and, with inBq, \"...\" inside "`...`".
func (l *shellLexer) double(inBq bool) {
	switch {
	case l.has(l.ph):
		l.emit(`${` + PathVar + `}`)
	case inBq && l.has(`\"`):
		l.pop()
		l.copyN(2)
	case l.src[l.i] == '\\':
		l.copyN(2)
	case !inBq && l.src[l.i] == '"':
		l.pop()
		l.copyN(1)
	case l.src[l.i] == '`' && !inBq:
		l.push(lexFrame{ctx: ctxBacktickDouble})
		l.copyN(1)
	case l.has("$("):
		l.substitution()
	default:
		l.copyN(1)
	}
}

// backtickInDouble handles "`...`": inner double quotes are written \"...\".
func (l *shellLexer) backtickInDouble() {
	switch {
	case l.has(l.ph):
		l.emit(`\"${` + PathVar + `}\"`)
	case l.has(`\"`):
		l.push(lexFrame{ctx: ctxDoubleInBq})
		l.copyN(2)
	case l.src[l.i] == '\\':
		l.copyN(2)
	case l.src[l.i] == '`':
		l.pop()
		l.copyN(1)
	case l.src[l.i] == '\'':
		l.push(lexFrame{ctx: ctxSingle})
		l.copyN(1)
	default:
		l.copyN(1)
	}
}

// arith handles $(( ... )) and (( ... )): no here-documents or case in there.
func (l *shellLexer) arith() {
	f := l.top()
	switch {
	case l.has(l.ph) && f.arithCmd:
		// "((cmd) ...)" is a nested subshell in POSIX sh; bash accepts quotes in (( )).
		l.emit(`"${` + PathVar + `}"`)
	case l.has(l.ph):
		l.emit(`${` + PathVar + `}`) // $(( )) does not allow quotes
	case f.arithCmd && l.src[l.i] == '\\':
		l.copyN(2)
	case f.arithCmd && l.src[l.i] == '\'':
		l.push(lexFrame{ctx: ctxSingle})
		l.copyN(1)
	case f.arithCmd && l.src[l.i] == '"':
		l.push(lexFrame{ctx: ctxDouble})
		l.copyN(1)
	case l.has("$("):
		l.substitution()
	case l.src[l.i] == '(':
		f.parens++
		l.copyN(1)
	case l.src[l.i] == ')':
		if f.parens > 0 {
			f.parens--
			l.copyN(1)
		} else {
			l.pop() // "))"
			l.copyN(2)
		}
	default:
		l.copyN(1)
	}
}

func (l *shellLexer) plain() {
	f := l.top()
	c := l.src[l.i]
	inSub := len(l.stack) > 1 && f.ctx == ctxPlain
	caseState := -1
	if n := len(f.cases); n > 0 {
		caseState = f.cases[n-1]
	}
	switch {
	case l.has(l.ph):
		l.emit(`"${` + PathVar + `}"`)
	case c == '\\':
		l.copyN(2)
	case c == '#' && l.atWordStart():
		for l.i < len(l.src) && l.src[l.i] != '\n' {
			l.copyN(1)
		}
	case c == '\n':
		l.copyN(1)
		l.startHeredocs()
	case c == '\'':
		l.push(lexFrame{ctx: ctxSingle})
		l.copyN(1)
	case l.has("$'"):
		l.push(lexFrame{ctx: ctxAnsi})
		l.copyN(2)
	case c == '"':
		l.push(lexFrame{ctx: ctxDouble})
		l.copyN(1)
	case c == '`':
		if f.ctx == ctxBacktick {
			l.pop()
		} else {
			l.push(lexFrame{ctx: ctxBacktick})
		}
		l.copyN(1)
	case l.has("$("):
		l.substitution()
	case l.has("((") && l.atWordStart() && l.atCommandStart():
		l.push(lexFrame{ctx: ctxArith, arithCmd: true})
		l.copyN(2)
	case l.has("<<<"): // here-string, not a here-document
		l.copyN(3)
	case l.has("<<"):
		l.heredocOperator()
	case l.word("case") && l.atCommandStart():
		f.cases = append(f.cases, caseWord)
		l.copyN(len("case"))
	case caseState == caseWord && l.word("in"):
		f.cases[len(f.cases)-1] = casePattern
		l.copyN(len("in"))
	case caseState >= casePattern && l.word("esac") && (l.atCommandStart() || l.prevWord() == "in"):
		f.cases = f.cases[:len(f.cases)-1]
		l.copyN(len("esac"))
	case caseState == caseBody && (l.has(";;") || l.has(";&")):
		f.cases[len(f.cases)-1] = casePattern
		if l.has(";;&") {
			l.copyN(3)
		} else {
			l.copyN(2)
		}
	case caseState == casePattern && c == '(':
		l.copyN(1) // optional "(" before a pattern
	case caseState == casePattern && c == ')':
		f.cases[len(f.cases)-1] = caseBody
		l.copyN(1)
	case c == '(' && inSub:
		f.parens++
		l.copyN(1)
	case c == ')' && inSub:
		if f.parens > 0 {
			f.parens--
		} else {
			l.pop()
		}
		l.copyN(1)
	default:
		l.copyN(1)
	}
}

// heredocOperator copies `<<[-] word` and queues the here-document.
func (l *shellLexer) heredocOperator() {
	l.copyN(2)
	h := heredoc{}
	if l.has("-") {
		h.stripTabs = true
		l.copyN(1)
	}
	for l.i < len(l.src) && (l.src[l.i] == ' ' || l.src[l.i] == '\t') {
		l.copyN(1)
	}
	var delim strings.Builder
	for l.i < len(l.src) && !isShellSep(l.src[l.i]) {
		switch c := l.src[l.i]; c {
		case '\'', '"':
			h.quoted = true
			l.copyN(1)
			for l.i < len(l.src) && l.src[l.i] != c {
				delim.WriteByte(l.src[l.i])
				l.copyN(1)
			}
			l.copyN(1)
		case '\\':
			h.quoted = true
			l.copyN(1)
			if l.i < len(l.src) {
				delim.WriteByte(l.src[l.i])
				l.copyN(1)
			}
		default:
			delim.WriteByte(c)
			l.copyN(1)
		}
	}
	h.delim = delim.String()
	l.pending = append(l.pending, h)
}

// lineIsDelim reports whether the line at the current position ends a
// here-document; it returns the line length including its newline.
func (l *shellLexer) lineIsDelim(delim string, stripTabs bool) (bool, int) {
	end := strings.IndexByte(l.src[l.i:], '\n')
	n := end + 1
	if end < 0 {
		end, n = len(l.src)-l.i, len(l.src)-l.i
	}
	line := l.src[l.i : l.i+end]
	if stripTabs {
		line = strings.TrimLeft(line, "\t")
	}
	return line == delim, n
}

// startHeredocs begins the bodies of the queued here-documents at the start of
// the current line: quoted bodies are copied as they are, an unquoted body is
// lexed in a ctxHeredoc frame (the next one starts when that frame ends).
func (l *shellLexer) startHeredocs() {
	for len(l.pending) > 0 {
		h := l.pending[0]
		l.pending = l.pending[1:]
		if !h.quoted {
			l.push(lexFrame{ctx: ctxHeredoc, delim: h.delim, stripTabs: h.stripTabs, lineStart: true})
			return
		}
		for l.i < len(l.src) {
			done, n := l.lineIsDelim(h.delim, h.stripTabs)
			l.copyN(n)
			if done {
				break
			}
		}
	}
}

// heredocBody lexes an unquoted here-document body: parameter expansion and
// command substitution apply, quotes are literal.
func (l *shellLexer) heredocBody() {
	f := l.top()
	if f.lineStart {
		f.lineStart = false
		if done, n := l.lineIsDelim(f.delim, f.stripTabs); done {
			l.copyN(n)
			l.pop()
			l.startHeredocs()
			return
		}
	}
	switch c := l.src[l.i]; {
	case l.has(l.ph):
		l.emit(`${` + PathVar + `}`)
	case c == '\\' && l.i+1 < len(l.src) && l.src[l.i+1] != '\n':
		l.copyN(2)
	case c == '\n':
		l.copyN(1)
		f.lineStart = true
	case c == '`':
		l.push(lexFrame{ctx: ctxBacktick})
		l.copyN(1)
	case l.has("$("):
		l.substitution()
	default:
		l.copyN(1)
	}
}
