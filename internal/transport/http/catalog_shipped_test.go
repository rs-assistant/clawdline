package http

import (
	"path/filepath"
	"testing"
)

// The catalogs this repository ships must pass the same validator /v1/strings
// applies, in every language. On 2026-10-17 one English string carried a
// literal "Settings > Machine > Voice input"; the validator reads a bare angle
// bracket as markup, so English itself was refused, /v1/strings answered 503
// for every language, and every console fell back to its bundled English
// without saying why. No test read the shipped files through this validator.
func TestEveryShippedCatalogIsServedInItsOwnLanguage(t *testing.T) {
	root := filepath.Join("..", "..", "..", "web", "console", "public")
	for _, lang := range []string{"en", "zh-Hant", "ja", "zh-Hans", "ko", "es", "pt-BR", "fr", "de"} {
		catalog, err := loadCatalog(root, lang)
		if err != nil {
			t.Fatalf("%s: %v", lang, err)
		}
		if got := catalog["lang"]; got != lang {
			t.Errorf("%s: served as %q; an invalid key makes the whole catalog fall back", lang, got)
		}
	}
}
