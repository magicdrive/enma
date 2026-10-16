package option_test

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/magicdrive/enma/internal/commandline/option"
)

func TestParseScheduler_Full(t *testing.T) {
	dir := t.TempDir()
	opt, err := option.ParseScheduler([]string{"--cron", "0 */10 * * * *", "--command", "make sync", "--name", "sync",
		"--working-dir", dir, "--timezone", "Asia/Tokyo", "--timeout", "30s", "--retries", "3", "--retry-delay", "10s",
		"--max-concurrent", "4"})
	if err != nil {
		t.Fatal(err)
	}
	if len(opt.Jobs) != 1 || opt.Location.String() != "Asia/Tokyo" || opt.MaxConcurrent != 4 {
		t.Fatalf("got %+v", opt)
	}
	j := opt.Jobs[0]
	if j.Name != "sync" || j.Command != "make sync" || j.WorkingDir != dir || j.Timeout != 30*time.Second ||
		j.Retries != 3 || j.RetryDelay != 10*time.Second || j.CronExpr != "0 */10 * * * *" {
		t.Fatalf("job: %+v", j)
	}
}

func TestParseScheduler_Defaults(t *testing.T) {
	opt, err := option.ParseScheduler([]string{"--cron", "* * * * * *", "--command", "true"})
	if err != nil {
		t.Fatal(err)
	}
	cwd, _ := os.Getwd()
	j := opt.Jobs[0]
	if j.Name != "default" || j.WorkingDir != cwd || j.Timeout != 0 || j.Retries != 0 || j.RetryDelay != 10*time.Second {
		t.Fatalf("job defaults: %+v", j)
	}
	if opt.MaxConcurrent != 4 || opt.Location != time.Local || opt.TimezoneName != "" {
		t.Fatalf("scheduler defaults: %+v", opt)
	}
}

func TestParseScheduler_Rejects(t *testing.T) {
	base := []string{"--cron", "* * * * * *", "--command", "true"}
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"--command", "true"}, "--cron: is required"},
		{[]string{"--cron", "* * * * * *"}, "--command: is required"},
		{append(base, "--cron", "* * * * *"), "--cron:"},
		{append(base, "--name", ""), "--name: must not be empty"},
		{append(base, "--timezone", "Nowhere/City"), "--timezone: unknown time zone"},
		{append(base, "--timeout", "-1s"), "--timeout:"},
		{append(base, "--retries", "-1"), "--retries: must not be negative"},
		{append(base, "--retries", "1", "--retry-delay", "0s"), "--retry-delay: must be positive"},
		{append(base, "--max-concurrent", "0"), "--max-concurrent: must be 1 or more"},
		{append(base, "--working-dir", "./no-such-dir"), "--working-dir:"},
		{append(base, "extra"), "unexpected argument"},
	}
	for _, tc := range cases {
		_, err := option.ParseScheduler(tc.args)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q: got %v, want %q", tc.args, err, tc.want)
		}
	}
}

func TestParseInit_SchedulerMode(t *testing.T) {
	t.Chdir(t.TempDir())
	opt, err := option.ParseInit([]string{"--mode", "scheduler"})
	if err != nil {
		t.Fatal(err)
	}
	if opt.ModeOpt != "scheduler" || opt.FileNameOpt != "Enma.toml" {
		t.Fatalf("got %+v", opt)
	}
}

func TestParseInit_HelpWithAnExistingFile(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.WriteFile("Enma.toml", []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	opt, err := option.ParseInit([]string{"--help"})
	if err != nil || !opt.HelpFlag {
		t.Fatalf("got %+v, %v", opt, err)
	}
	if _, err := option.ParseInit([]string{"--mode", "scheduler"}); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("existing file: got %v", err)
	}
}
