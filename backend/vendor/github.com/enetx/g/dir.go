package g

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// NewDir returns a new Dir instance with the given path.
func NewDir(path String) *Dir { return &Dir{path: path} }

// Chown changes the ownership of the directory to the specified UID and GID.
// It uses os.Chown to modify ownership and returns a Result[*Dir] indicating success or failure.
func (d *Dir) Chown(uid, gid int) Result[*Dir] {
	err := os.Chown(d.path.Std(), uid, gid)
	if err != nil {
		return Err[*Dir](err)
	}

	return Ok(d)
}

// Stat retrieves information about the directory represented by the Dir instance.
// It returns a Result[fs.FileInfo] containing details about the directory's metadata.
func (d *Dir) Stat() Result[fs.FileInfo] {
	if d.Path().IsErr() {
		return Err[fs.FileInfo](d.Path().err)
	}

	return ResultOf(os.Stat(d.Path().v.Std()))
}

// Lstat retrieves information about the symbolic link represented by the Dir instance.
// It returns a Result[fs.FileInfo] containing details about the symbolic link's metadata.
// Unlike Stat, Lstat does not follow the link and provides information about the link itself.
func (d *Dir) Lstat() Result[fs.FileInfo] { return ResultOf(os.Lstat(d.Path().v.Std())) }

// IsLink checks if the directory is a symbolic link.
func (d *Dir) IsLink() bool {
	stat := d.Lstat()
	return stat.IsOk() && stat.v.Mode()&os.ModeSymlink != 0
}

// CreateTemp creates a new temporary directory in the specified directory with the
// specified name pattern and returns a Result, which contains a pointer to the Dir
// or an error if the operation fails.
// If no directory is specified, the default directory for temporary directories is used.
// If no name pattern is specified, the default pattern "*" is used.
//
// Parameters:
//
// - args ...String: A variadic parameter specifying the directory and/or name
// pattern for the temporary directory.
//
// Returns:
//
// - *Dir: A pointer to the Dir representing the temporary directory.
//
// Example usage:
//
// d := g.NewDir("")
// tmpdir := d.CreateTemp()                     // Creates a temporary directory with default settings
// tmpdirWithDir := d.CreateTemp("mydir")       // Creates a temporary directory in "mydir" directory
// tmpdirWithPattern := d.CreateTemp("", "tmp") // Creates a temporary directory with "tmp" pattern
func (*Dir) CreateTemp(args ...String) Result[*Dir] {
	dir := ""
	pattern := "*"

	if len(args) != 0 {
		if len(args) > 1 {
			pattern = args[1].Std()
		}

		dir = args[0].Std()
	}

	tmpDir, err := os.MkdirTemp(dir, pattern)
	if err != nil {
		return Err[*Dir](err)
	}

	return Ok(NewDir(String(tmpDir)))
}

// Temp returns the default directory to use for temporary files.
//
// On Unix systems, it returns $TMPDIR if non-empty, else /tmp.
// On Windows, it uses GetTempPath, returning the first non-empty
// value from %TMP%, %TEMP%, %USERPROFILE%, or the Windows directory.
// On Plan 9, it returns /tmp.
//
// The directory is neither guaranteed to exist nor have accessible
// permissions.
func (*Dir) Temp() *Dir { return NewDir(String(os.TempDir())) }

// Remove attempts to delete the directory and its contents.
// It returns a Result, which contains either the *Dir or an error.
// If the directory does not exist, Remove returns a successful Result with *Dir set.
// Any error that occurs during removal will be of type *PathError.
func (d *Dir) Remove() Result[*Dir] {
	if err := os.RemoveAll(d.String().Std()); err != nil {
		return Err[*Dir](err)
	}

	return Ok(d)
}

// Copy copies the contents of the current directory to the destination directory.
//
// Parameters:
//
// - dest (String): The destination directory where the contents of the current directory should be copied.
//
// - followLinks (optional): A boolean indicating whether to follow symbolic links during the walk.
// If true, symbolic links are followed; otherwise, they are skipped.
//
// Returns:
//
// - Result[*Dir]: A Result type containing either a pointer to a new Dir instance representing the destination directory or an error.
//
// Example usage:
//
//	sourceDir := g.NewDir("path/to/source")
//	destinationDirResult := sourceDir.Copy("path/to/destination")
//	if destinationDirResult.IsErr() {
//		// Handle error
//	}
//	destinationDir := destinationDirResult.Ok()
func (d *Dir) Copy(dest String, followLinks ...bool) Result[*Dir] {
	files := NewSlice[*File]()

	for r := range d.Walk() {
		if r.IsErr() {
			return Err[*Dir](r.err)
		}
		files.Push(r.v)
	}

	root := d.Path()
	if root.IsErr() {
		return Err[*Dir](root.err)
	}

	follow := Slice[bool](followLinks).Get(0).UnwrapOr(true)

	for f := range files.Iter() {
		path := f.Path()
		if path.IsErr() {
			return Err[*Dir](path.err)
		}

		relpath, err := filepath.Rel(root.v.Std(), path.v.Std())
		if err != nil {
			return Err[*Dir](err)
		}

		destpath := NewDir(dest).Join(String(relpath))
		if destpath.IsErr() {
			return Err[*Dir](destpath.err)
		}

		stat := f.Stat()
		if stat.IsErr() {
			return Err[*Dir](stat.err)
		}

		if stat.v.IsDir() {
			if !follow && f.IsLink() {
				continue
			}

			if r := NewDir(destpath.v).CreateAll(stat.v.Mode()); r.IsErr() {
				return r
			}

			continue
		}

		if r := f.Copy(destpath.v, stat.v.Mode()); r.IsErr() {
			return Err[*Dir](r.err)
		}
	}

	return Ok(NewDir(dest))
}

// Create creates a new directory with the specified mode (optional).
//
// Parameters:
//
// - mode (os.FileMode, optional): The file mode for the new directory.
// If not provided, it defaults to DirDefault (0755).
//
// Returns:
//
// - *Dir: A pointer to the Dir instance on which the method was called.
//
// Example usage:
//
//	dir := g.NewDir("path/to/directory")
//	createdDir := dir.Create(0755) // Optional mode argument
func (d *Dir) Create(mode ...os.FileMode) Result[*Dir] {
	dmode := Slice[os.FileMode](mode).Get(0).UnwrapOr(DirDefault)
	if err := os.Mkdir(d.path.Std(), dmode); err != nil {
		return Err[*Dir](err)
	}

	return Ok(d)
}

// Join joins the current directory path with the given path elements, returning the joined path.
//
// Parameters:
//
// - elem (...String): One or more String values representing path elements to
// be joined with the current directory path.
//
// Returns:
//
// - String: The resulting joined path as an String.
//
// Example usage:
//
//	dir := g.NewDir("path/to/directory")
//	joinedPath := dir.Join("subdir", "file.txt")
func (d *Dir) Join(elem ...String) Result[String] {
	path := d.Path()
	if path.IsErr() {
		return Err[String](path.err)
	}

	se := SliceOf(elem...)
	se.Insert(0, path.v)

	return Ok(String(filepath.Join(se.ToStringSlice()...)))
}

// SetPath sets the path of the current directory.
//
// Parameters:
//
// - path (String): The new path to be set for the current directory.
//
// Returns:
//
// - *Dir: A pointer to the updated Dir instance with the new path.
//
// Example usage:
//
//	dir := g.NewDir("path/to/directory")
//	dir.SetPath("new/path/to/directory")
func (d *Dir) SetPath(path String) *Dir {
	d.path = path
	return d
}

// CreateAll creates all directories along the given path, with the specified mode (optional).
//
// Parameters:
//
// - mode ...os.FileMode (optional): The file mode to be used when creating the directories.
// If not provided, it defaults to the value of DirDefault constant (0755).
//
// Returns:
//
// - *Dir: A pointer to the Dir instance representing the created directories.
//
// Example usage:
//
//	dir := g.NewDir("path/to/directory")
//	dir.CreateAll()
//	dir.CreateAll(0755)
func (d *Dir) CreateAll(mode ...os.FileMode) Result[*Dir] {
	if d.Exist() {
		return Ok(d)
	}

	path := d.Path()
	if path.IsErr() {
		return Err[*Dir](path.err)
	}

	dmode := Slice[os.FileMode](mode).Get(0).UnwrapOr(DirDefault)

	err := os.MkdirAll(path.v.Std(), dmode)
	if err != nil {
		return Err[*Dir](err)
	}

	return Ok(d)
}

// Rename renames the current directory to the new path.
//
// Parameters:
//
// - newpath String: The new path for the directory.
//
// Returns:
//
// - *Dir: A pointer to the Dir instance representing the renamed directory.
// If an error occurs, the original Dir instance is returned with the error stored in d.err,
// which can be checked using the Error() method.
//
// Example usage:
//
//	dir := g.NewDir("path/to/directory")
//	dir.Rename("path/to/new_directory")
func (d *Dir) Rename(newpath String) Result[*Dir] {
	ps := String(os.PathSeparator)

	np := newpath.StripSuffix(ps).Split(ps).Collect()
	_ = np.Pop()

	if rd := NewDir(np.Join(ps)).CreateAll(); rd.IsErr() {
		return rd
	}

	if err := os.Rename(d.path.Std(), newpath.Std()); err != nil {
		return Err[*Dir](err)
	}

	return Ok(NewDir(newpath))
}

// Move function simply calls [Dir.Rename]
func (d *Dir) Move(newpath String) Result[*Dir] { return d.Rename(newpath) }

// Path returns the absolute path of the current directory.
//
// Returns:
//
// - String: The absolute path of the current directory as an String.
// If an error occurs while converting the path to an absolute path,
// the error is stored in d.err, which can be checked using the Error() method.
//
// Example usage:
//
//	dir := g.NewDir("path/to/directory")
//	absPath := dir.Path()
func (d *Dir) Path() Result[String] {
	path, err := filepath.Abs(d.path.Std())
	if err != nil {
		return Err[String](err)
	}

	return Ok(String(path))
}

// Exist checks if the current directory exists.
//
// Returns:
//
// - bool: true if the current directory exists, false otherwise.
//
// Example usage:
//
//	dir := g.NewDir("path/to/directory")
//	exists := dir.Exist()
func (d *Dir) Exist() bool {
	path := d.Path()
	if path.IsErr() {
		return false
	}

	_, err := os.Stat(path.v.Std())

	return !os.IsNotExist(err)
}

// Read iterates over the content of the current directory and yields File instances for each entry.
// This method uses a lazy evaluation strategy where each file is processed one at a time as it is needed.
//
// Returns:
//   - SeqResult[*File]: A sequence of Result[*File] instances representing each file and directory
//     in the current directory. It returns an error if reading the directory fails.
//
// Example usage:
//
//	dir := g.NewDir("path/to/directory")
//	files := dir.Read()
//	for file := range files {
//	    fmt.Println(file.Ok().Name())
//	}
func (d *Dir) Read() SeqResult[*File] {
	return func(yield func(Result[*File]) bool) {
		entries, err := os.ReadDir(d.path.Std())
		if err != nil {
			yield(Err[*File](err))
			return
		}

		dpath := d.Path()
		if dpath.IsErr() {
			yield(Err[*File](dpath.err))
			return
		}

		base := dpath.v

		for _, entry := range entries {
			full := NewDir(base).Join(String(entry.Name()))
			if full.IsErr() {
				yield(Err[*File](full.err))
				return
			}

			if !yield(Ok(NewFile(full.v))) {
				return
			}
		}
	}
}

// Glob iterates over files in the current directory matching a specified pattern and yields File instances for each match.
// This method utilizes a lazy evaluation strategy, processing files as they are needed.
//
// Returns:
//   - SeqResult[*File]: A sequence of Result[*File] instances representing the files that match the
//     provided pattern in the current directory. It returns an error if the glob operation fails.
//
// Example usage:
//
//	dir := g.NewDir("path/to/directory/*.txt")
//	files := dir.Glob()
//	for file := range files {
//	    fmt.Println(file.Ok().Name())
//	}
func (d *Dir) Glob() SeqResult[*File] {
	return (func(yield func(Result[*File]) bool) {
		matches, err := filepath.Glob(d.path.Std())
		if err != nil {
			yield(Err[*File](err))
			return
		}

		for _, match := range matches {
			file := NewFile(String(match)).Path()
			if file.IsErr() {
				yield(Err[*File](file.err))
				return
			}

			if !yield(Ok(NewFile(file.v))) {
				return
			}
		}
	})
}

// Walk returns a lazy sequence of all files and directories under the current Dir.
// You can customize inclusion/exclusion using SeqResult methods (Exclude, Filter, etc.).
//
// Example usage:
//
//	NewDir("path/to/dir").
//	  Walk().
//	  Exclude((*File).IsLink).
//	  ForEach(func(r Result[*File]) {
//	      if r.IsOk() {
//	          fmt.Println(r.Ok().Path().Ok().Std())
//	      }
//	  })
func (d *Dir) Walk() SeqResult[*File] {
	return func(yield func(Result[*File]) bool) {
		stack := SliceOf(d)

		for stack.NotEmpty() {
			current := stack.Pop()
			if current.IsNone() {
				break
			}

			current.v.Read().Range(func(r Result[*File]) bool {
				if r.IsErr() {
					return yield(r)
				}

				file := r.v
				if !yield(Ok(file)) {
					return false
				}

				stat := file.Stat()
				if stat.IsErr() {
					return yield(Err[*File](stat.err))
				}

				if stat.v.IsDir() {
					path := file.Path()
					if path.IsErr() {
						return yield(Err[*File](path.err))
					}

					stack.Push(NewDir(path.v))
				}

				return true
			})
		}
	}
}

// String returns the String representation of the current directory's path.
func (d *Dir) String() String { return d.path }

// Print writes the content of the Dir to the standard output (console)
// and returns the Dir unchanged.
func (d *Dir) Print() *Dir { fmt.Print(d); return d }

// Println writes the content of the Dir to the standard output (console) with a newline
// and returns the Dir unchanged.
func (d *Dir) Println() *Dir { fmt.Println(d); return d }
