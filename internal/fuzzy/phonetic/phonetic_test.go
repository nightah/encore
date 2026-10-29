package phonetic

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

type fixed string

func (f fixed) Keys(dst []string, _ string) []string { return append(dst, string(f)) }

func TestRegistry(t *testing.T) {
	saved := registry
	registry = map[string]Encoder{}

	t.Cleanup(func() { registry = saved })

	assert.Nil(t, For("fr"), "nothing registered and no English fallback")

	Register("en", fixed("EN"))
	Register("fr", fixed("FR"))

	for code, want := range map[string]Encoder{
		"fr": fixed("FR"), "fr-FR": fixed("FR"), "FR_ca": fixed("FR"), "en-AU": fixed("EN"), "de": fixed("EN"), "": fixed("EN"),
	} {
		assert.Equal(t, want, For(code), code)
	}

	assert.PanicsWithValue(t, `phonetic: encoder for "fr" registered twice`, func() { Register("fr", fixed("again")) })
}

func TestIsDigits(t *testing.T) {
	for word, want := range map[string]bool{"": false, "0": true, "2049": true, "20a": false, "²": false, "-1": false} {
		assert.Equal(t, want, IsDigits(word), word)
	}
}
