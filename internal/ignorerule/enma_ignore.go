package ignorerule

import (
	"bufio"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type IgnorePattern struct {
	Regexp *regexp.Regexp
	Negate bool
	LineNo int
	Raw    string
}

type GitIgnore struct {
	patterns []*IgnorePattern
}

func NewPlainIgnoreRule() *GitIgnore {
	return &GitIgnore{patterns: []*IgnorePattern{}}
}

func CompileIgnoreText(text string) (*GitIgnore, error) {
	return AppendIgnoreText(NewPlainIgnoreRule(), text)
}

func AppendIgnoreText(gi *GitIgnore, text string) (*GitIgnore, error) {
	var lines []string
	scanner := bufio.NewScanner(strings.NewReader(text))
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return AppendIgnoreLines(gi, lines...)
}

func CompileIgnoreLines(lines ...string) (*GitIgnore, error) {
	gi := NewPlainIgnoreRule()
	return AppendIgnoreLines(gi, lines...)
}

func AppendIgnoreFile(gi *GitIgnore, path string) (*GitIgnore, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return AppendIgnoreLines(gi, lines...)
}

// AppendIgnoreLines adds patterns; as in .gitignore the last matching pattern
// decides, so a repeated line is kept (it may override a negation before it).
// A line that does not compile (e.g. the class "[z-a]") is reported and skipped;
// the other lines still apply.
func AppendIgnoreLines(gi *GitIgnore, lines ...string) (*GitIgnore, error) {
	for i, raw := range lines {
		line := trimIgnoreLine(strings.TrimRight(raw, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		negate := false // a leading `\!` is a literal "!" (handled by the translation)
		if strings.HasPrefix(line, "!") {
			negate = true
			line = line[1:]
		}

		reStr := gitignorePatternToRegex(line)
		re, err := regexp.Compile(reStr)
		if err != nil {
			log.Printf("⚠️  ignore pattern %q (line %d) is invalid and skipped: %v", raw, i+1, err)
			continue
		}

		gi.patterns = append(gi.patterns, &IgnorePattern{
			Regexp: re,
			Negate: negate,
			LineNo: i + 1,
			Raw:    raw,
		})
	}
	return gi, nil
}

// trimIgnoreLine removes leading spaces (as enma always did) and trailing
// spaces unless the last one is escaped with a backslash (as in .gitignore).
func trimIgnoreLine(line string) string {
	line = strings.TrimLeft(line, " \t")
	trimmed := strings.TrimRight(line, " \t")
	if len(trimmed) < len(line) {
		// an odd number of backslashes at the end escapes the first space
		n := len(trimmed) - len(strings.TrimRight(trimmed, `\`))
		if n%2 == 1 {
			return trimmed + " "
		}
	}
	return trimmed
}

// gitignorePatternToRegex translates one .gitignore-style pattern into a
// regexp matched against a slash-separated path relative to the directory the
// patterns belong to (the project directory):
//   - a leading or middle slash anchors the pattern to that directory; without
//     one it matches at any depth;
//   - a trailing slash matches the directory and everything below it;
//   - `*` and `?` do not cross a slash, `**` does (`**/x`, `x/**`, `a/**/b`);
//   - `[abc]`, `[a-z]` and `[!abc]` are character classes;
//   - a backslash makes the next character literal (`\#`, `\!`, `\*`, `\ `).
func gitignorePatternToRegex(pattern string) string {
	dirOnly := strings.HasSuffix(pattern, "/") && !strings.HasSuffix(pattern, `\/`)
	if dirOnly {
		pattern = strings.TrimRight(pattern, "/")
	}
	anchor := strings.HasPrefix(pattern, "/")
	pattern = strings.TrimPrefix(pattern, "/")
	if strings.Contains(pattern, "/") {
		anchor = true
	}

	var b strings.Builder
	segs := strings.Split(pattern, "/")
	needSlash := false
	for i, seg := range segs {
		if seg == "**" {
			switch {
			case i == len(segs)-1 && i == 0:
				b.WriteString(`.*`)
			case i == len(segs)-1: // x/**: everything below x, not x itself (as in git)
				b.WriteString(`/.*`)
			default: // **/x, a/**/b: any number of directories
				if needSlash {
					b.WriteString("/")
				}
				b.WriteString(`(?:.*/)?`)
			}
			needSlash = false
			continue
		}
		if needSlash {
			b.WriteString("/")
		}
		b.WriteString(globSegmentToRegex(seg))
		needSlash = true
	}
	re := b.String()
	if dirOnly {
		re += `(?:/.*)?`
	}
	if anchor {
		return "^" + re + "$"
	}
	return "(^|/)" + re + "$"
}

// globSegmentToRegex translates one path segment of a pattern.
func globSegmentToRegex(seg string) string {
	var b strings.Builder
	for i := 0; i < len(seg); i++ {
		c := seg[i]
		switch c {
		case '\\':
			if i+1 < len(seg) {
				i++
				b.WriteString(regexp.QuoteMeta(seg[i : i+1]))
			} else {
				b.WriteString(`\\`)
			}
		case '*':
			for i+1 < len(seg) && seg[i+1] == '*' { // a ** inside a segment acts like *
				i++
			}
			b.WriteString(`[^/]*`)
		case '?':
			b.WriteString(`[^/]`)
		case '[':
			if class, n, ok := bracketClass(seg[i:]); ok {
				b.WriteString(class)
				i += n - 1
			} else {
				b.WriteString(`\[`)
			}
		default:
			b.WriteString(regexp.QuoteMeta(seg[i : i+1]))
		}
	}
	return b.String()
}

// bracketClass translates the bracket expression at the start of s ("[...]")
// and returns the regexp class and the bytes it used; ok is false when s has
// no closing bracket (the "[" is then literal).
func bracketClass(s string) (class string, n int, ok bool) {
	i := 1
	negate := false
	if i < len(s) && (s[i] == '!' || s[i] == '^') {
		negate = true
		i++
	}
	var body strings.Builder
	first := true
	for ; i < len(s); i++ {
		c := s[i]
		if c == ']' && !first {
			if body.Len() == 0 {
				return "", 0, false
			}
			if negate {
				return "[^/" + body.String() + "]", i + 1, true
			}
			return "[" + body.String() + "]", i + 1, true
		}
		first = false
		if c == '\\' && i+1 < len(s) { // an escaped character is literal
			i++
			c = s[i]
		} else if c == '-' { // a range, as in a-z
			body.WriteByte(c)
			continue
		}
		// letters and digits stand for themselves; anything else is escaped, so
		// that no character becomes a regexp escape such as \d
		if c < 0x80 && !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9') {
			body.WriteByte('\\')
		}
		body.WriteByte(c)
	}
	return "", 0, false
}

func CompileIgnoreFile(path string) (*GitIgnore, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return CompileIgnoreLines(lines...)
}

func (gi *GitIgnore) Matches(path string) bool {
	match, _ := gi.MatchesPathHow(path)
	return match
}

func (gi *GitIgnore) MatchesPathHow(path string) (bool, *IgnorePattern) {
	norm := filepath.ToSlash(path)
	matched := false
	var last *IgnorePattern
	for _, p := range gi.patterns {
		if p.Regexp.MatchString(norm) {
			matched = !p.Negate
			last = p
		}
	}
	return matched, last
}
