package ignorerule

import (
	"bufio"
	"os"
)

func ReadFilesAsLines(files []string) ([]string, error) {
	var lines []string
	for _, path := range files {
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer f.Close()

		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			lines = append(lines, scanner.Text())
		}

		if err := scanner.Err(); err != nil {
			return nil, err
		}
	}
	return lines, nil
}

func NewGitignore(fileList []string) (*GitIgnore, error) {
	var fileLine []string
	var err error
	fileLine, err = ReadFilesAsLines(fileList)

	if err != nil {
		return nil, err
	}
	return CompileIgnoreLines(fileLine...)

}
