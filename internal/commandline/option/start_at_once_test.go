package option_test

import (
	"testing"

	"github.com/magicdrive/enma/internal/commandline/option"
)

func TestParse_StartAtOnce(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"unset", nil, ""},
		{"empty", []string{"--start-at-once", ""}, ""},
		{"set", []string{"--start-at-once", "make prepare"}, "make prepare"},
		{"equals form", []string{"--start-at-once=make prepare"}, "make prepare"},
		{"single dash", []string{"-start-at-once", "echo {}"}, "echo {}"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w, err := option.ParseWatch(append([]string{"--command", "make test"}, tc.args...))
			if err != nil {
				t.Fatal(err)
			}
			h, err := option.ParseHotload(append([]string{"--daemon", "./app", "--build", "go build"}, tc.args...))
			if err != nil {
				t.Fatal(err)
			}
			if w.StartAtOnce != tc.want || h.StartAtOnce != tc.want {
				t.Fatalf("got watch %q hotload %q, want %q", w.StartAtOnce, h.StartAtOnce, tc.want)
			}
			// start-at-once is independent of the other commands
			if w.Cmd != "make test" || w.PreCmd != "" || w.PostCmd != "" {
				t.Fatalf("watch commands changed: %+v", w)
			}
			if h.Build != "go build" || h.PreBuild != "" || h.PostBuild != "" || h.BuildAtStart.String() != "on" {
				t.Fatalf("hotload commands changed: %+v", h)
			}
		})
	}
}
