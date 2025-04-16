package model

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/magicdrive/enma/internal/textbank"
)

type ArgsPathStyleString string

type ArgsPathStyleObj struct {
	DirNameFlag   bool
	BaseNameFlag  bool
	ExtensionFlag bool
}

const (
	DIRNAME   = "dirname"
	BASENAME  = "basename"
	EXTENSION = "extension"
)

var styleNameUnitMap = map[string]string{
	"d":         DIRNAME,
	"dir":       DIRNAME,
	"dirname":   DIRNAME,
	"dirName":   DIRNAME,
	"DirName":   DIRNAME,
	"Dirname":   DIRNAME,
	"dir_name":  DIRNAME,
	"b":         BASENAME,
	"base":      BASENAME,
	"basename":  BASENAME,
	"baseName":  BASENAME,
	"BaseName":  BASENAME,
	"Basename":  BASENAME,
	"base_name": BASENAME,
	"e":         EXTENSION,
	"extension": EXTENSION,
	"ext":       EXTENSION,
}

func (m *ArgsPathStyleString) Set(value string) error {
	fmt.Println(value)
	dict := map[string]string{}
	for s := range strings.SplitSeq(value, ",") {
		part := strings.TrimSpace(s)
		if styleName, ok := styleNameUnitMap[part]; ok {
			if _, exist := dict[styleName]; exist {
				return fmt.Errorf("invalid value: %s. Allowed values exist once part name.\n%s",
					value, textbank.ShortHelpMessage)
			} else {
				dict[styleNameUnitMap[part]] = "!"
			}
		} else {
			return fmt.Errorf("invalid value: %s. Allowed values must match part name comma separated.\n%s",
				value, textbank.ShortHelpMessage)
		}
	}

	*m = ArgsPathStyleString(value)
	return nil
}

func (m *ArgsPathStyleString) String() string {
	return string(*m)
}

func (m *ArgsPathStyleString) ArgsPathStyleObj() (*ArgsPathStyleObj, error) {
	result := &ArgsPathStyleObj{}
	for part := range strings.SplitSeq(m.String(), ",") {
		if styleName, ok := styleNameUnitMap[part]; ok {
			switch styleName {
			case DIRNAME:
				result.DirNameFlag = true
			case BASENAME:
				result.BaseNameFlag = true
			case EXTENSION:
				result.ExtensionFlag = true
			default:
				return nil, fmt.Errorf("invalid args-path-style name: %s", styleName)
			}
		}
	}
	return result, nil
}

func (m *ArgsPathStyleObj) ArgsPathString(path string) string {
	var b strings.Builder

	dir := filepath.Dir(path)
	file := filepath.Base(path)
	ext := filepath.Ext(file)
	name := strings.TrimSuffix(file, ext)

	// OS-specific fix
	if (dir == "." || dir == "..") && !filepath.IsAbs(path) {
		dir = ""
	}

	// Windows: ensure \ ends dir, Unix: /
	if m.DirNameFlag && dir != "" {
		b.WriteString(dir)
		if runtime.GOOS == "windows" {
			if !strings.HasSuffix(dir, "\\") {
				b.WriteString("\\")
			}
		} else {
			if !strings.HasSuffix(dir, "/") {
				b.WriteString("/")
			}
		}
	}

	if m.BaseNameFlag {
		b.WriteString(name)
	}
	if m.ExtensionFlag {
		b.WriteString(ext)
	}

	return b.String()
}
