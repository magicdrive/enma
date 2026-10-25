package main

// Consistency tests for the release path: `make publish` (hephaestus
// publish) and the workflow conventions it relies on
// (https://github.com/magicdrive/hephaestus/blob/main/docs/publish.md,
// "Conventions").

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// TestMakefile_Publish drives the publish targets with PUBLISH replaced by a
// stub that records how it was called: argument handling, make -n and
// make -j, without any git or GitHub access.
func TestMakefile_Publish(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the Makefile targets macOS and Linux")
	}
	if _, err := exec.LookPath("make"); err != nil {
		t.Skip("make not installed")
	}
	dir := t.TempDir()
	log := filepath.Join(dir, "calls")
	stub := filepath.Join(dir, "stub")
	script := "#!/bin/sh\nprintf '%s|%s|%s|%s\\n' \"$*\" \"$HEPHAESTUS_PUBLISH_ARGS\" \"$HEPHAESTUS_PUBLISH_VERSION\" \"$HEPHAESTUS_PUBLISH_TAG\" >> '" + log + "'\n"
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	mk := func(env []string, args ...string) (string, []string, error) {
		os.Remove(log)
		cmd := exec.Command("make", append([]string{"--no-print-directory", "PUBLISH=" + stub}, args...)...)
		// A clean make environment: no MAKEFLAGS or jobserver from a parent
		// make, and no VERSION/YES/tag unless the case sets them.
		for _, kv := range os.Environ() {
			k, _, _ := strings.Cut(kv, "=")
			if !slices.Contains([]string{"MAKEFLAGS", "MFLAGS", "MAKELEVEL", "VERSION", "YES", "tag"}, k) && !strings.HasPrefix(k, "HEPHAESTUS_PUBLISH_") {
				cmd.Env = append(cmd.Env, kv)
			}
		}
		cmd.Env = append(cmd.Env, env...)
		out, err := cmd.CombinedOutput()
		calls, _ := os.ReadFile(log)
		var lines []string
		for _, l := range strings.Split(strings.TrimSpace(string(calls)), "\n") {
			if l != "" {
				lines = append(lines, l)
			}
		}
		return string(out), lines, err
	}

	for _, tc := range []struct {
		name string
		env  []string
		args []string
		want string // the one stub call: "args|ARGS|VERSION|TAG"
	}{
		{"positional", nil, []string{"publish", "v1.0.0"}, "publish -from-make|v1.0.0||"},
		{"prerelease", nil, []string{"publish", "v1.0.0-rc.1"}, "publish -from-make|v1.0.0-rc.1||"},
		{"VERSION=", nil, []string{"publish", "VERSION=v1.0.0"}, "publish -from-make||v1.0.0|"},
		{"tag= (as before)", nil, []string{"publish", "tag=v1.0.0"}, "publish -from-make|||v1.0.0"},
		{"both reach the tool, which rejects them", nil, []string{"publish", "v1.0.0", "VERSION=v1.0.0"}, "publish -from-make|v1.0.0|v1.0.0|"},
		{"no version reaches the tool, which rejects it", nil, []string{"publish"}, "publish -from-make|||"},
		{"YES=1 on the command line", nil, []string{"publish", "v1.0.0", "YES=1"}, "publish -from-make -yes|v1.0.0||"},
		{"YES other than 1 does not confirm", nil, []string{"publish", "v1.0.0", "YES=yes"}, "publish -from-make|v1.0.0||"},
		{"YES, VERSION and tag from the environment are ignored", []string{"YES=1", "VERSION=v9.9.9", "tag=v9.9.8"}, []string{"publish", "v1.0.0"}, "publish -from-make|v1.0.0||"},
		{"check", nil, []string{"publish-check", "v1.0.0"}, "check -from-make|v1.0.0||"},
		{"status", nil, []string{"publish-status", "v1.0.0"}, "status -from-make|v1.0.0||"},
		{"watch", nil, []string{"publish-watch", "v1.0.0"}, "watch -from-make|v1.0.0||"},
		{"help", nil, []string{"publish-help"}, "help|||"},
		{"parallel make runs the tool once", nil, []string{"-j4", "publish", "v1.0.0"}, "publish -from-make|v1.0.0||"},
		// Were the goal pasted into the recipe, the shell would run false.
		{"shell metacharacters stay data", nil, []string{"publish", "v1;false"}, "publish -from-make|v1;false||"},
		{"shell metacharacters in tag= stay data", nil, []string{"publish", "tag=v1;false"}, "publish -from-make|||v1;false"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, calls, err := mk(tc.env, tc.args...)
			if err != nil {
				t.Fatalf("make %v: %v\n%s", tc.args, err, out)
			}
			if len(calls) != 1 || calls[0] != tc.want {
				t.Fatalf("make %v called the tool %q, want once %q\n%s", tc.args, calls, tc.want, out)
			}
		})
	}

	// refused by make before the tool runs
	for _, args := range [][]string{
		{"publish", "v1.0.0", "v2.0.0"},
		{"build", "publish", "v1.0.0"},
		{"test", "publish", "v1.0.0"},
		{"publish", "publish-check", "v1.0.0"},
		{"publish-help", "v1.0.0"},
		{"publish", "clean"},
		{"publish", "vet"},
		{"publish", "latest"},
		{"publish-check", "v1.0.0", "smoke"},
	} {
		out, calls, err := mk(nil, args...)
		if err == nil || len(calls) != 0 {
			t.Errorf("make %v: err %v, calls %q\n%s", args, err, calls, out)
		}
	}

	out, calls, err := mk(nil, "-n", "publish", "v1.0.0")
	if err != nil || len(calls) != 0 || !strings.Contains(out, "publish -from-make") {
		t.Errorf("make -n ran the tool (calls %q, err %v)\n%s", calls, err, out)
	}
}

// TestMakefile_PublishDefault: without PUBLISH, the publish targets run the
// pinned Hephaestus (`hephaestus publish`), not anything else.
func TestMakefile_PublishDefault(t *testing.T) {
	mk := readRepoFile(t, "Makefile")
	for _, want := range []string{
		"\nPUBLISH ?= $(HEPHAESTUS) publish\n",
		"\nHEPHAESTUS ?= go run github.com/magicdrive/hephaestus@$(HEPHAESTUS_VERSION)\n",
	} {
		if !strings.Contains(mk, want) {
			t.Errorf("Makefile has no line %q", strings.TrimSpace(want))
		}
	}
	if regexp.MustCompile(`(?m)^\s+.*git (tag|push)\b`).MatchString(mk) {
		t.Error("the Makefile must not create or push tags itself; hephaestus publish does")
	}
}

// workflowSteps splits the steps of a workflow job ("  <job>:") into their
// text, one entry per "- name:" step.
func workflowSteps(t *testing.T, workflow, job string) []string {
	t.Helper()
	src := readRepoFile(t, workflow)
	start := strings.Index(src, "\n  "+job+":\n")
	if start < 0 {
		t.Fatalf("%s has no job %s", workflow, job)
	}
	body := src[start+len(job)+4:]
	if next := regexp.MustCompile(`\n  [a-z][a-z0-9-]*:\n`).FindStringIndex(body); next != nil {
		body = body[:next[0]]
	}
	parts := regexp.MustCompile(`(?m)^      - name: `).Split(body, -1)
	return parts[1:]
}

// TestReleaseWorkflow_TapTokenOnlyWhenTapPublished: the tap token reaches
// only steps that run when the plan says the tap is published, so a
// prerelease never has it; the two steps hephaestus publish verifies by name
// exist; every release asset is attested.
func TestReleaseWorkflow_TapTokenOnlyWhenTapPublished(t *testing.T) {
	const cond = "if: steps.plan.outputs.tap == 'publish'"
	steps := workflowSteps(t, ".github/workflows/release.yml", "release")
	src := readRepoFile(t, ".github/workflows/release.yml")
	if !regexp.MustCompile(`\n  release:\n(    .*\n)*    environment: release\n`).MatchString(src) {
		t.Error("the release job must run in environment release")
	}
	plan := -1
	var names []string
	for i, s := range steps {
		name, _, _ := strings.Cut(s, "\n")
		names = append(names, name)
		if strings.Contains(s, "\n        id: plan\n") {
			plan = i
			if !strings.Contains(s, "uses: magicdrive/hephaestus@") || !strings.Contains(s, "command: plan") {
				t.Errorf("the plan step must run the Hephaestus action with command: plan:\n%s", s)
			}
		}
		if !strings.Contains(s, "secrets.HOMEBREW_TAP_GITHUB_TOKEN") {
			continue
		}
		if plan < 0 {
			t.Errorf("step %q uses the tap token before the plan step", name)
		}
		if !strings.Contains(s, "\n        "+cond+"\n") {
			t.Errorf("step %q gets the tap token without %q", name, cond)
		}
	}
	if plan < 0 {
		t.Fatal("the release job has no plan step (id: plan)")
	}
	for _, n := range []string{
		"Rehearse the formula and tap pull request (read-only)",
		"Generate the formula and open a tap pull request",
	} {
		if !slices.Contains(names, n) {
			t.Errorf("the release job has no step %q (hephaestus publish verifies it by name)", n)
		}
	}
	if c := strings.Count(src, "secrets.HOMEBREW_TAP_GITHUB_TOKEN"); c != 3 {
		t.Errorf("release.yml names the tap token %d times; want 3 (the set check, the rehearsal, the tap step)", c)
	}
	for _, subject := range []string{"dist/enma_*.tar.gz", "dist/enma_*.zip", "dist/enma_checksums.txt"} {
		if !strings.Contains(src, "            "+subject+"\n") {
			t.Errorf("release.yml does not attest %s", subject)
		}
	}
}

// TestCIWorkflow_Gate: ci.yml has the job ci-gate (the required check of the
// main ruleset), which needs every other job and fails unless all succeeded.
func TestCIWorkflow_Gate(t *testing.T) {
	src := readRepoFile(t, ".github/workflows/ci.yml")
	jobsAt := strings.Index(src, "\njobs:\n")
	if jobsAt < 0 {
		t.Fatal("ci.yml has no jobs")
	}
	var jobs []string
	for _, m := range regexp.MustCompile(`(?m)^  ([a-z][a-z0-9-]*):$`).FindAllStringSubmatch(src[jobsAt:], -1) {
		if m[1] != "ci-gate" {
			jobs = append(jobs, m[1])
		}
	}
	gate := regexp.MustCompile(`\n  ci-gate:\n(?:    .*\n|\s*\n)*?    needs: \[([^\]]*)\]\n`).FindStringSubmatch(src)
	if gate == nil {
		t.Fatal("ci.yml has no ci-gate job with needs: [...]")
	}
	var needs []string
	for _, n := range strings.Split(gate[1], ",") {
		needs = append(needs, strings.TrimSpace(n))
	}
	slices.Sort(jobs)
	slices.Sort(needs)
	if !slices.Equal(jobs, needs) {
		t.Errorf("ci-gate needs %v; want every other job %v", needs, jobs)
	}
	body := src[strings.Index(src, "\n  ci-gate:\n"):]
	for _, want := range []string{"    if: always()\n", "${{ join(needs.*.result, ' ') }}", `[ "$r" = success ] || exit 1`} {
		if !strings.Contains(body, want) {
			t.Errorf("ci-gate has no %q", want)
		}
	}
}

func readRepoFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
