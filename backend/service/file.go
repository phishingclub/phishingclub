package service

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path"

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

// Delete deletes a file inside root
func (f *File) Delete(
	root *os.Root,
	relativePath string,
) error {
	err := root.Remove(relativePath)
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

// RemoveEmptyFolderRecursively removes dir and then each parent folder inside
// root while they are empty. It stops at the root, at a folder that is not
// empty, and at anything that is not a real folder such as a symlink.
func (f *File) RemoveEmptyFolderRecursively(
	root *os.Root,
	dir string,
) error {
	for dir != "." && dir != "" && dir != "/" {
		info, err := root.Lstat(dir)
		if err != nil || !info.IsDir() {
			f.Logger.Debugw("not a folder inside the root, stopping", "path", dir)
			return nil
		}
		// a folder that is not empty fails to remove, which ends the walk.
		if err := root.Remove(dir); err != nil {
			f.Logger.Debugw("folder not removed, stopping", "path", dir, "error", err)
			return nil
		}
		f.Logger.Debugw("deleted empty folder", "path", dir)
		dir = path.Dir(dir)
	}
	return nil
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

		dst, err := f.createInRoot(root, relativePath, false)
		if err != nil {
			return 0, err
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
	dst, err := f.createInRoot(root, relativePath, overwrite)
	if err != nil {
		return err
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

// createInRoot creates the parent folders and opens relativePath for writing
// inside root. Without overwrite the file is opened with O_EXCL, so an existing
// file is never replaced, even one that appears just before the open.
func (f *File) createInRoot(root *os.Root, relativePath string, overwrite bool) (*os.File, error) {
	if dir := path.Dir(relativePath); dir != "." {
		if err := root.MkdirAll(dir, 0755); err != nil {
			f.Logger.Errorw("failed to create directory", "path", dir, "error", err)
			return nil, errs.Wrap(err)
		}
	}
	flag := os.O_WRONLY | os.O_CREATE | os.O_EXCL
	if overwrite {
		flag = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	}
	dst, err := root.OpenFile(relativePath, flag, 0644)
	if err != nil {
		if os.IsExist(err) {
			f.Logger.Debugw("file already exists", "path", relativePath)
			return nil, errs.NewValidationError(fmt.Errorf("file already exists: %s", relativePath))
		}
		f.Logger.Errorw("failed to create file", "path", relativePath, "error", err)
		return nil, errs.Wrap(err)
	}
	return dst, nil
}
