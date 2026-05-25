package static

import (
	"embed"
	"io/fs"
	"net/http"
	"os"
)

//go:embed all:public
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

// FileExists reports whether the named file exists under the public
// root. Dev mode (publicDir set + exists) consults disk; production
// consults the embedded FS. Used by the /office-v2 handler to decide
// between serving the React shell and a placeholder page.
func FileExists(publicDir, name string) bool {
	if publicDir != "" {
		if info, err := os.Stat(publicDir); err == nil && info.IsDir() {
			if _, err := os.Stat(publicDir + "/" + name); err == nil {
				return true
			}
			return false
		}
	}
	publicSub, err := fs.Sub(publicFS, "public")
	if err != nil {
		return false
	}
	f, err := publicSub.Open(name)
	if err != nil {
		return false
	}
	_ = f.Close()
	return true
}
