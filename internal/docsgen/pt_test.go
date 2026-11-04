package docsgen

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPortugueseVoicePage(t *testing.T) {
	files, err := Generate(root)
	require.NoError(t, err)

	page, ok := files[ContentDir+"/voice-commands/pt.md"]
	require.True(t, ok, "no voice commands page for pt")

	assert.Contains(t, string(page), "title: Portuguese (Português)\n")
	assert.Contains(t, string(page), "editUrl: https://crowdin.com/project/encore/pt-BR\n", "Crowdin names Portuguese pt-BR")
	assert.Contains(t, string(page), "| `pt-BR` | Encore | kodi |")
}
