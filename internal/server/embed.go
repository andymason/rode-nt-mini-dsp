package server

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed static/*
var staticFiles embed.FS

// StaticHandler returns an http.Handler that serves the embedded static files.
func StaticHandler() (http.Handler, error) {
	// Get the embedded filesystem for the static directory
	fsys, err := fs.Sub(staticFiles, "static")
	if err != nil {
		return nil, err
	}

	return http.FileServer(http.FS(fsys)), nil
}

// GetStaticFS returns the embedded filesystem for static files.
func GetStaticFS() fs.FS {
	return staticFiles
}

// StaticExists checks if a static file exists.
func StaticExists(path string) bool {
	data, err := staticFiles.ReadFile("static/" + path)
	return err == nil && len(data) > 0
}
