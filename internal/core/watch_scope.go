package core

import (
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
)

// watchScope places paths for the watch rules (hotload and watch). Rules
// (--exclude-dir, .enmaignore) look at a path relative to the project
// directory, or to the watch directory holding it, so the directories above
// the project never match them: a project inside ~/.enma/, vendor/ or tmp/ is
// watched like any other.
type watchScope struct {
	once  sync.Once
	base  string   // project directory: the working directory, symlinks resolved
	roots []string // watch directories, absolute, symlinks resolved
}

// init fixes the project directory (the current directory: Start runs after
// the switch to --working-dir) and the watch directories. Only the first call counts.
func (s *watchScope) init(watchDirs []string) {
	s.once.Do(func() {
		s.base = realPath(".")
		for _, d := range watchDirs {
			if r := realPath(d); r != "" {
				s.roots = append(s.roots, r)
			}
		}
	})
}

// realPath returns path absolute with symlinks resolved, or only absolute when
// it cannot be resolved (it may not exist any more); "" when neither works.
func realPath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return ""
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		return real
	}
	return abs
}

// within returns path relative to dir (slash-separated) when path is dir or
// below it. Both are clean absolute paths. On macOS and Windows, whose file
// systems usually ignore case, the comparison does too (the relative part keeps
// path's spelling).
func within(dir, path string) (string, bool) {
	if samePath(dir, path) {
		return ".", true
	}
	prefix := dir
	if !strings.HasSuffix(prefix, string(filepath.Separator)) {
		prefix += string(filepath.Separator)
	}
	if len(path) > len(prefix) && samePath(path[:len(prefix)], prefix) {
		return filepath.ToSlash(path[len(prefix):]), true
	}
	return "", false
}

func samePath(a, b string) bool {
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// rel returns path relative to the project directory, or to the watch
// directory that holds it, slash-separated ("." for the directory itself).
// A path outside both is returned whole (absolute, slash-separated).
func (s *watchScope) rel(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	candidates := []string{abs}
	if real, err := filepath.EvalSymlinks(filepath.Dir(abs)); err == nil {
		candidates = append(candidates, filepath.Join(real, filepath.Base(abs)))
	}
	for _, p := range candidates {
		if s.base != "" {
			if r, ok := within(s.base, p); ok {
				return r
			}
		}
		for _, root := range s.roots {
			if r, ok := within(root, p); ok {
				return r
			}
		}
	}
	return filepath.ToSlash(abs)
}

// inRoots reports whether path (symlinks resolved) is a watch directory or below one.
func (s *watchScope) inRoots(path string) bool {
	for _, root := range s.roots {
		if _, ok := within(root, path); ok {
			return true
		}
	}
	return false
}

// hasExcludedElement reports whether a directory name of the slash path rel
// is in excluded. With self, the last element counts too (rel is a directory).
func hasExcludedElement(rel string, excluded []string, self bool) bool {
	if !self {
		rel = filepath.ToSlash(filepath.Dir(filepath.FromSlash(rel)))
	}
	for name := range strings.SplitSeq(rel, "/") {
		if name != "" && name != "." && slices.Contains(excluded, name) {
			return true
		}
	}
	return false
}

// collectWatchDirs returns root and the directories below it (symlinks resolved,
// each once) that skip does not reject; skip gets each directory's resolved path
// and a rejected directory is not descended into. Symlinked directories are not
// followed. A directory below root that cannot be read is reported and left
// out; only an unreadable root is an error.
func collectWatchDirs(root string, skip func(dir string) bool) ([]string, error) {
	var result []string
	visited := make(map[string]bool)
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if path == root {
				return err
			}
			abs, _ := filepath.Abs(path)
			log.Printf("⚠️  Cannot read %s, not watching it: %v", abs, err)
			if d != nil && d.IsDir() {
				// WalkDir reports a directory it cannot list after visiting it: drop it
				if real := realPath(path); len(result) > 0 && result[len(result)-1] == real {
					result = result[:len(result)-1]
					delete(visited, real)
				}
				return filepath.SkipDir
			}
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		real := realPath(path)
		if real == "" || visited[real] {
			return filepath.SkipDir
		}
		if path != root && skip(real) {
			return filepath.SkipDir
		}
		visited[real] = true
		result = append(result, real)
		return nil
	})
	return result, err
}

// symlinkedDirToFollow returns the directory a symlink created at runtime points
// to, when it should be watched: only inside the watch directories (it is then
// usually watched already); one pointing elsewhere is reported and not followed.
func (s *watchScope) symlinkedDirToFollow(link string) (string, bool) {
	resolved, err := filepath.EvalSymlinks(link)
	if err != nil {
		return "", false
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.IsDir() {
		return "", false
	}
	if !s.inRoots(resolved) {
		log.Printf("⚠️  Not watching %s: the symlink points outside the watch directories (%s)", link, resolved)
		return "", false
	}
	return resolved, true
}
