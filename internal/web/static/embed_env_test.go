package static

import (
	"os"
	"testing"
)

func TestPublicDirFromEnv(t *testing.T) {
	const def = "internal/web/static/public"

	t.Run("unset returns default", func(t *testing.T) {
		os.Unsetenv("OFFICE_SPA_DIR")
		if got := PublicDirFromEnv(def); got != def {
			t.Fatalf("want %q, got %q", def, got)
		}
	})

	t.Run("set overrides default", func(t *testing.T) {
		t.Setenv("OFFICE_SPA_DIR", "/app/office-public/current")
		if got := PublicDirFromEnv(def); got != "/app/office-public/current" {
			t.Fatalf("override not applied, got %q", got)
		}
	})

	t.Run("empty falls back to default", func(t *testing.T) {
		t.Setenv("OFFICE_SPA_DIR", "")
		if got := PublicDirFromEnv(def); got != def {
			t.Fatalf("want %q, got %q", def, got)
		}
	})
}
