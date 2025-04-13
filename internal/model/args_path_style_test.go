package model_test

import (
	"path/filepath"
	"testing"

	"github.com/magicdrive/enma/internal/model"
)

func TestArgsPathStyleString_Set_Valid(t *testing.T) {
	cases := []string{
		"d,b,e",
		"dir,base,ext",
		"dirname,basename,extension",
		"dirName,baseName,extension",
		"DirName,BaseName,extension",
		"Dirname,Basename,extension",
		"dir_name,base_name,extension",
		"b",
		"e,d",
	}

	for _, input := range cases {
		var s model.ArgsPathStyleString
		if err := s.Set(input); err != nil {
			t.Errorf("expected success for %q, got error: %v", input, err)
		}
	}
}

func TestArgsPathStyleString_Set_Invalid(t *testing.T) {
	cases := []string{
		"d,b,e,b",   // duplicate
		"unknown",   // invalid keyword
		"b,unknown", // partially invalid
	}

	for _, input := range cases {
		var s model.ArgsPathStyleString
		if err := s.Set(input); err == nil {
			t.Errorf("expected error for %q, but got none", input)
		}
	}
}

func TestArgsPathStyleString_ArgsPathStyleObj(t *testing.T) {
	var s model.ArgsPathStyleString = "d,b,e"
	obj, err := s.ArgsPathStyleObj()
	if err != nil {
		t.Errorf("expected success for %q, got error: %v", s, err)
	}

	if !obj.DirNameFlag || !obj.BaseNameFlag || !obj.ExtensionFlag {
		t.Errorf("expected all flags true, got %+v", obj)
	}
}

func TestArgsPathString(t *testing.T) {
	type testCase struct {
		styleStr string
		path     string
		expected string
	}

	cases := []testCase{
		{"d,b,e", "/path/to/file.txt", "/path/to/file.txt"},
		{"b,e", "/path/to/file.txt", "file.txt"},
		{"b", "/path/to/file.txt", "file"},
		{"e", "/path/to/file.txt", ".txt"},
		{"d", "/path/to/file.txt", "/path/to/"},
		{"d,b", "/file", "/file"},
		{"d,e", "/file.txt", "/.txt"},
		{"b,e", "./file.txt", "file.txt"},
		{"d,b,e", ".", "."},
		{"d,b,e", "/path/to/file.txt", "/path/to/file.txt"},
		{"b", "/space path/to/file hoge.txt", "file hoge"},
		{"e", "/space path/to/file hoge.txt", ".txt"},
		{"d", "/space path/to/file hoge.txt", "/space path/to/"},
		{"e", filepath.FromSlash(`C:/Users/Alice/file.txt`), `.txt`},
		{"b", filepath.FromSlash("C:/Users/Alice/file.txt"), "file"},
		{"d", filepath.FromSlash("C:/Users/Alice/file.txt"), filepath.FromSlash("C:/Users/Alice/")},
		{"d,b,e", filepath.FromSlash("C:/Users/Alice/file.txt"), filepath.FromSlash("C:/Users/Alice/file.txt")},
	}

	for _, c := range cases {
		var style model.ArgsPathStyleString
		if err := style.Set(c.styleStr); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		obj, err := style.ArgsPathStyleObj()
		if err != nil {
			t.Errorf("expected success for %q, got error: %v", c.styleStr, err)
		}
		output := obj.ArgsPathString(c.path)
		if output != c.expected {
			t.Errorf("for style=%q path=%q expected=%q, got=%q",
				c.styleStr, c.path, c.expected, output)
		}
	}
}
