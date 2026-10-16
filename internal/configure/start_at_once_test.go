package configure_test

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/pelletier/go-toml/v2"

	"github.com/magicdrive/enma/internal/configure"
)

func decode(t *testing.T, doc string) *configure.TomlConfig {
	t.Helper()
	var cfg configure.TomlConfig
	if err := toml.Unmarshal([]byte(doc), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return &cfg
}

func TestStartAtOnce_TOMLKeys(t *testing.T) {
	cases := []struct {
		name, keys, want, err string
	}{
		{"unset", ``, "", ""},
		{"empty", `start_at_once = ""`, "", ""},
		{"key", `start_at_once = "make prepare"`, "make prepare", ""},
		{"cli spelling", `start-at-once = "make prepare"`, "make prepare", ""},
		{"both equal", "start_at_once = \"make prepare\"\nstart-at-once = \"make prepare\"", "make prepare", ""},
		{"both differ", "start_at_once = \"a\"\nstart-at-once = \"b\"", "", "both set"},
		{"placeholder kept", `start_at_once = "echo {} $1"`, "echo {} $1", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := decode(t, "[subcommand.watch]\ncommand = \"true\"\n"+tc.keys+
				"\n[subcommand.hotload]\ndaemon = \"./app\"\nbuild = \"true\"\n"+tc.keys+"\n")
			w, werr := configure.NewWatchOptionFromTOMLConfig(cfg.Subcommand.Watch)
			h, herr := configure.NewHotloadOptionFromTOMLConfig(cfg.Subcommand.Hotload)
			for mode, err := range map[string]error{"watch": werr, "hotload": herr} {
				if tc.err != "" {
					if err == nil || !strings.Contains(err.Error(), tc.err) {
						t.Fatalf("%s: got %v, want error %q", mode, err, tc.err)
					}
				} else if err != nil {
					t.Fatalf("%s: %v", mode, err)
				}
			}
			if tc.err != "" {
				return
			}
			if w.StartAtOnce != tc.want || h.StartAtOnce != tc.want {
				t.Fatalf("got watch %q hotload %q, want %q", w.StartAtOnce, h.StartAtOnce, tc.want)
			}
		})
	}
}

// The existing keys keep their meaning next to start_at_once.
func TestStartAtOnce_DoesNotChangeOtherKeys(t *testing.T) {
	doc := "[subcommand.hotload]\ndaemon = \"./app\"\nbuild = \"go build\"\npre_build = \"p\"\npost_build = \"q\"\nbuild_at_start = false\ntimeout = \"9sec\"\nretry = 2\n"
	without, err := configure.NewHotloadOptionFromTOMLConfig(decode(t, doc).Subcommand.Hotload)
	if err != nil {
		t.Fatal(err)
	}
	with, err := configure.NewHotloadOptionFromTOMLConfig(decode(t, doc+"start_at_once = \"s\"\n").Subcommand.Hotload)
	if err != nil {
		t.Fatal(err)
	}
	with.StartAtOnce = ""
	with.FlagSet, without.FlagSet = nil, nil
	with.EnmaIgnore, without.EnmaIgnore = nil, nil
	if *with.ArgsPathStyle != *without.ArgsPathStyle {
		t.Fatal("args path style changed")
	}
	with.ArgsPathStyle, without.ArgsPathStyle = nil, nil
	if !equalOptions(with, without) {
		t.Fatalf("start_at_once changed other fields:\n%+v\n%+v", with, without)
	}
}

// FuzzStartAtOnceTOML: any string written as start_at_once (TOML-escaped) reaches
// the option unchanged; a blank one (which runs nothing) may become "".
func FuzzStartAtOnceTOML(f *testing.F) {
	for _, s := range []string{"make prepare", "", " ", "echo {} \"$1\"", "a\nb", "'\\\"", "\x00", "日本語"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if !utf8.ValidString(s) {
			return // a TOML string is UTF-8: not representable
		}
		doc, err := toml.Marshal(map[string]any{"subcommand": map[string]any{
			"watch": map[string]any{"command": "true", "start_at_once": s},
		}})
		if err != nil {
			t.Fatalf("marshal %q: %v", s, err)
		}
		var cfg configure.TomlConfig
		if err := toml.Unmarshal(doc, &cfg); err != nil {
			t.Fatalf("unmarshal %q: %v", doc, err)
		}
		if cfg.Subcommand.Watch.StartAtOnce != s {
			t.Fatalf("TOML round trip: got %q, want %q", cfg.Subcommand.Watch.StartAtOnce, s)
		}
		opt, err := configure.NewWatchOptionFromTOMLConfig(cfg.Subcommand.Watch)
		if err != nil {
			t.Fatalf("%q: %v", s, err)
		}
		want := cfg.Subcommand.Watch.StartAtOnce
		if strings.TrimSpace(want) == "" {
			want = ""
		}
		if opt.StartAtOnce != want {
			t.Fatalf("got %q, want %q", opt.StartAtOnce, want)
		}
	})
}

// FuzzStartAtOnceKeys: the resolution of start_at_once / start-at-once.
func FuzzStartAtOnceKeys(f *testing.F) {
	f.Add("a", "")
	f.Add("", "b")
	f.Add("a", "a")
	f.Add("a", "b")
	f.Add(" ", "b")
	f.Fuzz(func(t *testing.T, key, alias string) {
		got, err := configure.StartAtOnce(key, alias)
		keySet, aliasSet := strings.TrimSpace(key) != "", strings.TrimSpace(alias) != ""
		switch {
		case keySet && aliasSet && key != alias:
			if err == nil {
				t.Fatalf("(%q, %q): want an error", key, alias)
			}
		case err != nil:
			t.Fatalf("(%q, %q): %v", key, alias, err)
		case keySet && got != key:
			t.Fatalf("(%q, %q) = %q, want the key", key, alias, got)
		case !keySet && got != alias:
			t.Fatalf("(%q, %q) = %q, want the alias", key, alias, got)
		}
	})
}

func equalOptions(a, b any) bool { return fmt.Sprintf("%+v", a) == fmt.Sprintf("%+v", b) }
