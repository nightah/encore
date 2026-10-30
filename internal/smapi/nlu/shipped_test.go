package nlu_test

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/smapi/nlu"
)

// TestShippedFixtures keeps testdata/nlu in step with the model: one file per shipped locale, every row naming a real intent and its slots, every intent covered.
func TestShippedFixtures(t *testing.T) {
	src := sources(t)

	fixtures, err := nlu.Load(os.DirFS("../../../testdata/nlu"))
	require.NoError(t, err)

	var locales []string
	for _, f := range fixtures {
		locales = append(locales, f.Locale)

		require.NoError(t, f.Check(src))
		assert.Empty(t, f.Missing(src), "%s has no row for these intents", f.Locale)
	}

	assert.Equal(t, src.LocaleNames(), locales)
}

// TestShippedDutchFreeTextRows keeps the rows that prove Dutch fills AMAZON.SearchQuery: a bare title and each other free-text intent.
func TestShippedDutchFreeTextRows(t *testing.T) {
	f, err := nlu.LoadFile(os.DirFS("../../../testdata/nlu"), "nl-NL.yaml")
	require.NoError(t, err)

	want := map[string]string{"PlayMedia": "Query", "GlobalSearch": "Query", "TypeText": "Text", "OpenFavourite": "Favourite", "LaunchAddon": "Addon"}

	for _, c := range f.Cases {
		if slot, ok := want[c.Intent]; ok {
			if _, filled := c.Slots[slot]; filled {
				delete(want, c.Intent)
			}
		}
	}

	assert.Empty(t, want, "nl-NL needs a row filling each free-text slot")
}
