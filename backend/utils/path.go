package utils

import "path/filepath"

// GetSafePathWithinRoot returns a safe path within a root
// safeRootPath is the root directory and must be a SAFE string
// unsafePath is the path to be joined to the root and can be UNSAFE string
// example: GetSafePathWithinRoot("/home/user", "../etc/passwd") returns "/home/user/etc/passwd"
func GetSafePathWithinRoot(safeRootPath, unsafePath string) string {
	return filepath.Join(safeRootPath, filepath.Clean("/"+unsafePath))
}
