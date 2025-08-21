//go:build dev

package frontend

import "embed"

// In dev mode no files are embeded
// all files are served from the filesystem at runtime
var content embed.FS

// GetEmbededFS returns the embeded file system that contains the frontend
func GetEmbededFS() *embed.FS {
	return &content
}
