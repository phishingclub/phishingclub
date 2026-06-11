package service

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/phishingclub/phishingclub/errs"
)

// RootFileUpload is a file upload using os.Root
type RootFileUpload struct {
	Root         *os.Root
	RelativePath string
	File         *multipart.FileHeader
}

// NewRootFileUpload creates a new secure file upload
func NewRootFileUpload(root *os.Root, relativePath string, file *multipart.FileHeader) *RootFileUpload {
	return &RootFileUpload{
		Root:         root,
		RelativePath: relativePath,
		File:         file,
	}
}

// File is a File service
type File struct {
	Common
}

// Delete deletes a file
func (f *File) Delete(
	path string,
) error {
	err := os.Remove(path)
	if err != nil {
		f.Logger.Errorw("failed to delete file", "error", err)
		return err
	}
	return nil
}

// DeleteAll deletes a file or folder recursively
func (f *File) DeleteAll(
	path string,
) error {
	err := os.RemoveAll(path)
	if err != nil {
		f.Logger.Errorw("failed to delete path", "error", err)
		return err
	}
	return nil
}

// RemoveEmptyFolderRecursively folders recursively deletes all empty folders
// until it hits an non-empty folder or the root
func (f *File) RemoveEmptyFolderRecursively(
	rootPath string,
	path string,
) error {
	f.Logger.Debugw("Checking if empty folders should be removed, root: %s, path: %s",
		"rootPath", rootPath,
		"path", path,
	)

	// check if the path is the root
	if path == rootPath {
		f.Logger.Debug("path is the root, stopping recursion")
		return nil
	}
	// check if the path is empty
	entries, err := os.ReadDir(path)
	if err != nil {
		f.Logger.Errorw("failed to read directory", "error", err)
		return nil
	}
	if len(entries) > 0 {
		f.Logger.Debug("path is not empty, stopping recursion")
		return nil
	}
	// delete the empty folder
	f.Logger.Debugw("deleting empty folder", "path", path)
	err = os.Remove(path)
	if err != nil {
		f.Logger.Errorw("failed to delete empty folder", "error", err)
		return err
	}
	// check the parent folder
	parent := filepath.Dir(path)
	return f.RemoveEmptyFolderRecursively(rootPath, parent)
}

// Upload uploads files using os.Root for security
func (f *File) Upload(
	g *gin.Context,
	files []*RootFileUpload,
) (int, error) {
	for _, fileUpload := range files {
		root := fileUpload.Root
		relativePath := fileUpload.RelativePath
		file := fileUpload.File

		f.Logger.Debugw("checking if file path is valid", "path", relativePath)

		// validate path is safe through root
		_, err := root.Stat(relativePath)
		if err != nil && !os.IsNotExist(err) {
			f.Logger.Errorw("invalid file path", "path", relativePath, "error", err)
			return 0, errs.Wrap(err)
		}

		// check if file already exists
		pathDoesNotExist := os.IsNotExist(err)
		if !pathDoesNotExist {
			f.Logger.Debugw("file already exists", "path", relativePath)
			return 0, errs.NewValidationError(fmt.Errorf("file already exists: %s", relativePath))
		}

		// create directory structure if needed
		dirPath := filepath.Dir(relativePath)
		if dirPath != "." {
			// create directories step by step
			parts := strings.Split(dirPath, "/")
			currentPath := ""
			for _, part := range parts {
				if part != "" {
					if currentPath == "" {
						currentPath = part
					} else {
						currentPath = filepath.Join(currentPath, part)
					}

					// check if directory exists
					_, err = root.Stat(currentPath)
					if err != nil && !os.IsNotExist(err) {
						f.Logger.Errorw("invalid directory path", "path", currentPath, "error", err)
						return 0, errs.Wrap(err)
					}

					// create directory if it doesn't exist
					if os.IsNotExist(err) {
						err = root.Mkdir(currentPath, 0755)
						if err != nil {
							f.Logger.Errorw("failed to create directory", "path", currentPath, "error", err)
							return 0, errs.Wrap(err)
						}
						f.Logger.Debugw("created directory", "path", currentPath)
					}
				}
			}
		}

		// open file for writing through root
		dst, err := root.OpenFile(relativePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
		if err != nil {
			f.Logger.Errorw("failed to create file", "path", relativePath, "error", err)
			return 0, errs.Wrap(err)
		}
		defer dst.Close()

		// open uploaded file
		src, err := file.Open()
		if err != nil {
			f.Logger.Errorw("failed to open uploaded file", "error", err)
			return 0, errs.Wrap(err)
		}
		defer src.Close()

		// copy file content
		_, err = io.Copy(dst, src)
		if err != nil {
			f.Logger.Errorw("failed to copy file content", "error", err)
			return 0, errs.Wrap(err)
		}

		f.Logger.Debugw("file uploaded successfully", "path", relativePath)
	}
	return len(files), nil
}

// UploadFile uploads a file using os.Root for security
func (f *File) UploadFile(
	root *os.Root,
	relativePath string,
	contents *bytes.Buffer,
	overwrite bool,
) error {
	f.Logger.Debugw("checking if file path is valid", "path", relativePath)

	// validate path is safe through root
	_, err := root.Stat(relativePath)
	if err != nil && !os.IsNotExist(err) {
		f.Logger.Errorw("invalid file path", "path", relativePath, "error", err)
		return errs.Wrap(err)
	}

	// check if file exists
	pathDoesNotExist := os.IsNotExist(err)
	if !pathDoesNotExist && !overwrite {
		f.Logger.Debugw("file already exists and overwrite is false", "path", relativePath)
		return errs.NewValidationError(fmt.Errorf("file already exists: %s", relativePath))
	}

	// create directory structure if needed
	dirPath := filepath.Dir(relativePath)
	if dirPath != "." {
		// create directories step by step
		parts := strings.Split(dirPath, "/")
		currentPath := ""
		for _, part := range parts {
			if part != "" {
				if currentPath == "" {
					currentPath = part
				} else {
					currentPath = filepath.Join(currentPath, part)
				}

				// check if directory exists
				_, err = root.Stat(currentPath)
				if err != nil && !os.IsNotExist(err) {
					f.Logger.Errorw("invalid directory path", "path", currentPath, "error", err)
					return errs.Wrap(err)
				}

				// create directory if it doesn't exist
				if os.IsNotExist(err) {
					err = root.Mkdir(currentPath, 0755)
					if err != nil {
						f.Logger.Errorw("failed to create directory", "path", currentPath, "error", err)
						return errs.Wrap(err)
					}
					f.Logger.Debugw("created directory", "path", currentPath)
				}
			}
		}
	}

	// open file for writing through root
	dst, err := root.OpenFile(relativePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		f.Logger.Errorw("failed to create file", "path", relativePath, "error", err)
		return errs.Wrap(err)
	}
	defer dst.Close()

	// write content
	_, err = io.Copy(dst, contents)
	if err != nil {
		f.Logger.Errorw("failed to write file content", "error", err)
		return errs.Wrap(err)
	}

	f.Logger.Debugw("file uploaded successfully", "path", relativePath)
	return nil
}
