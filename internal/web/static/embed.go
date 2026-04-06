package static

import (
	"embed"
	"io/fs"
	"net/http"
	"os"
)

//go:embed public/*
var publicFS embed.FS

// Handler returns an http.Handler that serves static files.
// In dev mode (publicDir exists on disk), serves from filesystem for hot reload.
// In production, serves from the embedded filesystem.
func Handler(publicDir string) http.Handler {
	if publicDir != "" {
		if info, err := os.Stat(publicDir); err == nil && info.IsDir() {
			return http.FileServer(http.Dir(publicDir))
		}
	}

	// Production: serve from embedded files
	publicSub, err := fs.Sub(publicFS, "public")
	if err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "embedded files not available", http.StatusInternalServerError)
		})
	}
	return http.FileServer(http.FS(publicSub))
}
