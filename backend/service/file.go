package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/go-errors/errors"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/validate"
)

// FileUpload is a file upload
type FileUpload struct {
	Path string
	File *multipart.FileHeader
}

// RootFileUpload is a file upload using os.Root
type RootFileUpload struct {
	Root         *os.Root
	RelativePath string
	File         *multipart.FileHeader
}

// NewFileUpload creates a new file upload
func NewFileUpload(path string, file *multipart.FileHeader) *FileUpload {
	return &FileUpload{
		Path: path,
		File: file,
	}
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

// checkFilePathIsValidForUpload checks if the file path is valid for upload
func (f *File) checkFilePathIsValidForUpload(path string) error {
	parts := strings.Split(path, "/")
	// Check each part of the path
	for i := 1; i < len(parts); i++ {
		partPath := strings.Join(parts[:i], "/")
		info, err := os.Stat(partPath)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				// The path part does not exist, which is expected as we are still constructing the full path
				continue
			} else {
				// Some other error occurred
				return fmt.Errorf("failed to check asset (%s) path info: %w", partPath, err)
			}
		}
		if !info.IsDir() {
			return fmt.Errorf(
				"part of the path is a file: '%s' of '%s'",
				partPath,
				path,
			)
		}
	}
	return nil
}

func (f *File) UploadLegacy(
	g *gin.Context,
	files []*FileUpload,
) (int, error) {
	for _, fileUpload := range files {
		path := fileUpload.Path
		file := fileUpload.File
		f.Logger.Debugw("checking if file exists", "path", path)
		err := f.checkFilePathIsValidForUpload(path)
		if err != nil && !errors.Is(err, errs.ErrValidationFailed) {
			return 0, errs.Wrap(err)
		}
		if err != nil {
			f.Logger.Errorw("failed to check file path for upload", "error", err)
			return 0, errs.Wrap(err)
		}
		// check if the file exists
		_, err = os.Stat(path)
		pathDoesNotExists := errors.Is(err, fs.ErrNotExist)
		if err != nil && !pathDoesNotExists {
			f.Logger.Errorw("failed to get asset path info", "error", err)
			return 0, errs.Wrap(err)
		}
		// a file or folder already exists
		if !pathDoesNotExists {
			filePathNotExistsMsg := fmt.Sprintf("file already exists at '%s'", path)
			f.Logger.Debug(filePathNotExistsMsg)
			return 0, validate.WrapErrorWithField(
				errs.NewValidationError(
					errors.New("a file already exists with that name"),
				),
				"file",
			)
		}
		// Upload the file
		err = g.SaveUploadedFile(file, path)
		if err != nil {
			f.Logger.Errorw("failed to save uploaded file", "error", err)
			return 0, errs.Wrap(err)
		}
		f.Logger.Debugw("file uploaded", "error", path)
	}
	return len(files), nil
}

func (f *File) UploadFileLegacy(
	ctx context.Context,
	path string,
	contents *bytes.Buffer,
	overwrite bool,
) error {
	f.Logger.Debugw("checking if file exists", "path", path)
	err := f.checkFilePathIsValidForUpload(path)
	if err != nil && !errors.Is(err, errs.ErrValidationFailed) {
		return err
	}
	if err != nil {
		f.Logger.Errorw("failed to check file path for upload", "error", err)
		return err
	}
	// check if the file exists
	_, err = os.Stat(path)
	pathDoesNotExists := errors.Is(err, fs.ErrNotExist)
	if err != nil && !pathDoesNotExists {
		f.Logger.Errorw("failed to get asset path info", "error", err)
		return err
	}
	// a file or folder already exists
	if !overwrite && !pathDoesNotExists {
		filePathNotExistsMsg := fmt.Sprintf("file already exists at '%s'", path)
		f.Logger.Debug(filePathNotExistsMsg)
		return validate.WrapErrorWithField(
			errs.NewValidationError(
				errors.New("a file already exists with that name"),
			),
			"file",
		)
	}
	// Create directories if they don't exist
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0750); err != nil {
		f.Logger.Errorw("failed to create directories", "error", err)
		return err
	}
	if overwrite {
		f.Logger.Debug("removing existing file...")
		err = os.RemoveAll(path)
		if err != nil {
			f.Logger.Errorw("failed to remove existing file", "error", err)
			return err
		}
	}
	// #nosec
	outFile, err := os.Create(path)
	if err != nil {
		f.Logger.Errorw("failed to create file", "error", err)
		return err
	}

	// #nosec
	_, err = contents.WriteTo(outFile)
	_ = outFile.Close()
	if err != nil {
		f.Logger.Errorw("failed to write file", "error", err)
		return err
	}

	f.Logger.Debugw("file uploaded", "path", path)
	return nil
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
