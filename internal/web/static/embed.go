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

// PublicDirFromEnv returns the OFFICE_SPA_DIR env var when set (non-empty),
// otherwise def. This lets the SPA be served from a mounted volume in prod
// so it can be deployed independently of the Go binary, while an unset/dev
// environment keeps the repo-relative default. Handler() and FileExists()
// are already disk-first with an embedded-FS fallback, so a missing or
// empty dir degrades gracefully to the embedded build.
func PublicDirFromEnv(def string) string {
	if d := os.Getenv("OFFICE_SPA_DIR"); d != "" {
		return d
	}
	return def
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
