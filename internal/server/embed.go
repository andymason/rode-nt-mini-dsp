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
	fsys, err := fs.Sub(staticFiles, "static")
	if err != nil {
		return nil, err
	}
	return http.FileServer(http.FS(fsys)), nil
}
