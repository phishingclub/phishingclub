//go:build !dev

package frontend

import "embed"

//go:embed build/*
var content embed.FS

// GetEmbededFS returns the embeded file system that contains the frontend
func GetEmbededFS() *embed.FS {
	return &content
}
