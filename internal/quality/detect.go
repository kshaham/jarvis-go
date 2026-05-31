package quality

import (
	"os"
	"path/filepath"
)

// DetectLanguage inspects a project directory for ecosystem markers and
// returns the primary language (LangGo / LangNode / LangPython), or "" when
// none is recognized. Go is checked first, then Node, then Python — a repo
// with several markers is classified by its dominant build system in that
// order (matches how the office's agents treat polyglot repos).
func DetectLanguage(dir string) string {
	has := func(name string) bool {
		_, err := os.Stat(filepath.Join(dir, name))
		return err == nil
	}
	switch {
	case has("go.mod"):
		return LangGo
	case has("package.json"):
		return LangNode
	case has("pyproject.toml"), has("requirements.txt"), has("setup.py"), has("setup.cfg"):
		return LangPython
	}
	return ""
}
