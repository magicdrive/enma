package ignorerule

import (
	"os"
	"path/filepath"

	"github.com/magicdrive/enma/internal/model"
)

// GenerateIntegratedEnmaIgnore combines the default ignores, the .enmaignore
// of the working directory (the project directory; parent directories are not
// searched) and the listed files. Patterns match paths relative to the
// working directory.
func GenerateIntegratedEnmaIgnore(workingDir string, defaultIgnore model.IgnoreType, fileList []string) *GitIgnore {
	var gi, _ = CompileIgnoreText(defaultIgnore.EnmaignoreText())
	path := filepath.Join(workingDir, ".enmaignore")
	if _, err := os.Stat(path); err == nil {
		if res, err := AppendIgnoreFile(gi, path); err == nil {
			gi = res
		}
	}
	if res, err := AppendIgnoreFileList(gi, workingDir, fileList); err == nil {
		gi = res
	}
	return gi
}
