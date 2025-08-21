package file

import (
	"fmt"
	"io/fs"
	"os"
)

// Write writes data to a file
type Writer interface {
	Write(filepath string, data []byte, flag int, perm fs.FileMode) (int, error)
}

// Write writes data to a file
// returns bytes written or error
func Write(filepath string, data []byte, flag int, perm fs.FileMode) (int, error) {
	// #nosec
	file, err := os.OpenFile(filepath, flag, perm)
	if err != nil {
		return 0, fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()
	b, err := file.Write(data)
	if err != nil {
		return b, fmt.Errorf("failed to write to file: %w", err)
	}
	return b, nil
}

// FileWriter is a file writer
type FileWriter struct{}

// Write writes data to a file
func (w FileWriter) Write(filepath string, data []byte, flag int, perm fs.FileMode) (int, error) {
	return Write(filepath, data, flag, perm)
}
