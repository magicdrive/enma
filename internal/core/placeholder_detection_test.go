//go:build !windows

package core_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/magicdrive/enma/internal/commandline/option"
	"github.com/magicdrive/enma/internal/core"
)

// A-08: PreBuild / PostBuild are skipped without a path only when they really
// use the placeholder; an escaped or commented-out one does not count.
func TestPlaceholderDetection_StepsWithoutPath(t *testing.T) {
	cases := []struct {
		name, suffix string
		runs         bool
	}{
		{"real placeholder skips", `; : {}`, false},
		{"escaped placeholder runs", `; : \{}`, true},
		{"commented placeholder runs", ` # {}`, true},
	}
	for _, c := range cases {
		for _, step := range []string{"PreBuild", "PostBuild"} {
			t.Run(c.name+"/"+step, func(t *testing.T) {
				mark := filepath.Join(t.TempDir(), "ran")
				command := `touch '` + mark + `'` + c.suffix
				args := []string{"--daemon", "./myapp", "--build", "true", "--placeholder", "{}"}
				if step == "PreBuild" {
					args = append(args, "--pre-build", command)
				} else {
					args = append(args, "--post-build", command)
				}
				opt, err := option.ParseHotload(args)
				if err != nil {
					t.Fatal(err)
				}
				r := &core.HotloadRunner{Options: opt, BuildTimeout: 10 * time.Second, ExecCommand: exec.CommandContext}
				run := r.RunPreBuild
				if step == "PostBuild" {
					run = r.RunPostBuild
				}
				if err := run(context.Background(), ""); err != nil {
					t.Fatalf("%s failed: %v", step, err)
				}
				if _, err := os.Stat(mark); (err == nil) != c.runs {
					t.Fatalf("%s ran = %v, want %v", step, err == nil, c.runs)
				}
			})
		}
	}
}
