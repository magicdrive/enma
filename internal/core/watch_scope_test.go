package core_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/magicdrive/enma/internal/commandline/option"
	"github.com/magicdrive/enma/internal/core"
)

// A directory named in --exclude-dir or ignored by .enmaignore rules (here the
// default .git/) is not watched itself, nor anything below it, in both modes;
// directories above the project never count (F02).
func TestCollectWatchDirs_ExcludedDirItselfIsNotWatched(t *testing.T) {
	root, _ := filepath.EvalSymlinks(t.TempDir())
	proj := filepath.Join(root, "vendor", "proj")                                   // the project itself is below a "vendor"
	for _, d := range []string{"src", "vendor/pkg", "src/vendor", ".git/objects"} { // .git/: a default ignore
		if err := os.MkdirAll(filepath.Join(proj, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(proj)

	hopt := HelperInflateHotloadOptionDefaultValue(&option.HotloadOption{ExcludeDir: "vendor"})
	wopt := HelperInflateWatchOptionDefaultValue(&option.WatchOption{ExcludeDir: "vendor"})
	for _, err := range []error{hopt.Valid(), hopt.Normalize(), wopt.Valid(), wopt.Normalize()} {
		if err != nil {
			t.Fatal(err)
		}
	}
	h := &core.HotloadRunner{Options: hopt}
	w := &core.WatchRunner{Options: wopt}
	for name, collect := range map[string]func(string) ([]string, error){"hotload": h.CollectWatchDirs, "watch": w.CollectWatchDirs} {
		dirs, err := collect(proj)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		want := []string{proj, filepath.Join(proj, "src")}
		slices.Sort(dirs)
		if !slices.Equal(dirs, want) {
			t.Errorf("%s: watched %v, want %v", name, dirs, want)
		}
	}
}
