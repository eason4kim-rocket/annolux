package searcheval

import (
	"bufio"
	"errors"
	"os"
	"path/filepath"
)

const outputTemporaryPattern = ".searcheval-partial-*"

// WriteExclusive installs one completed round without replacing an earlier
// baseline. The parent directory must already exist.
func WriteExclusive(outputPath string, contents []byte) error {
	if len(contents) == 0 {
		return errors.New("evaluation output is empty")
	}
	if _, err := os.Lstat(outputPath); err == nil {
		return &os.PathError{Op: "create", Path: outputPath, Err: os.ErrExist}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	output, err := os.CreateTemp(filepath.Dir(outputPath), outputTemporaryPattern)
	if err != nil {
		return err
	}
	temporaryPath := output.Name()
	defer func() {
		_ = output.Close()
		_ = os.Remove(temporaryPath)
	}()
	buffered := bufio.NewWriterSize(output, 64<<10)
	if _, err := buffered.Write(contents); err != nil {
		return err
	}
	if err := buffered.Flush(); err != nil {
		return err
	}
	if err := output.Sync(); err != nil {
		return err
	}
	if err := output.Chmod(0o600); err != nil {
		return err
	}
	if err := output.Close(); err != nil {
		return err
	}
	return os.Link(temporaryPath, outputPath)
}
