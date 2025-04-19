package ignorerule

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/magicdrive/enma/internal/model"
)

func FindEnmaIgnorePath() (string, error) {

	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}

	for {
		path := filepath.Join(dir, ".enmaignore")
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}

		parentDir := filepath.Dir(dir)
		if parentDir == dir {
			break
		}
		dir = parentDir
	}

	return "", errors.New(".enmaignore not found")
}

func GenerateIntegratedEnmaIgnore(workingDir string, defaultIgnore model.IgnoreType, fileList []string) *GitIgnore {
	var gi, _ = CompileIgnoreText(defaultIgnore.EnmaignoreText())
	if path, err := FindEnmaIgnorePath(); err == nil {
		if res, err := AppendIgnoreFile(gi, path); err == nil {
			gi = res
		}
	}
	if res, err := AppendIgnoreFileList(gi, workingDir, fileList); err == nil {
		gi = res
	}
	return gi
}
