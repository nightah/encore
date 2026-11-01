package i18n

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/core/match"
	"github.com/nightah/encore/internal/skillmodel"
)

// TestEveryLanguageIsComplete keeps the skill, the matcher and the web interface in step: a language the skill speaks must have its own title-matching rules and interface translation, and the matcher must not have rules for a language the skill doesn't speak.
func TestEveryLanguageIsComplete(t *testing.T) {
	src, err := skillmodel.Load(FS)
	require.NoError(t, err)

	var spoken []string

	for _, name := range src.LocaleNames() {
		if lang := src.Locales[name].Language; !slices.Contains(spoken, lang) {
			spoken = append(spoken, lang)
		}
	}

	slices.Sort(spoken)
	assert.Equal(t, match.Languages(), spoken, "languages with matching rules against languages with locales")

	for _, lang := range spoken {
		info, err := os.Stat(filepath.Join("..", "web", "src", "i18n", "locales", lang))
		if assert.NoError(t, err, "%s has no web interface translation", lang) {
			assert.True(t, info.IsDir(), lang)
		}
	}
}
