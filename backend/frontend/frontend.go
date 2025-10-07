//go:build !dev

package frontend

import "embed"

//go:embed build/*
//go:embed all:build/_app
var content embed.FS

// GetEmbededFS returns the embeded file system that contains the frontend
func GetEmbededFS() *embed.FS {
	return &content
}
