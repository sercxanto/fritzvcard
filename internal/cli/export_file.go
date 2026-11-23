package cli

import (
	"errors"
	"io"
	"os"
	"path/filepath"
)

func writeExportFile(path string, write func(io.Writer) error) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".fritzvcard-export-*")
	if err != nil {
		return err
	}
	if err = write(f); err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		// On Windows os.Rename uses MoveFileEx with MOVEFILE_REPLACE_EXISTING.
		err = os.Rename(f.Name(), path)
	}
	if err != nil {
		if cleanupErr := os.Remove(f.Name()); cleanupErr != nil {
			return errors.Join(err, cleanupErr)
		}
	}
	return err
}
