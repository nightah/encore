package phonetic

import (
	"fmt"
	"strings"
)

// registry is written only by Register during package initialisation and read afterwards, so it needs no lock.
var registry = map[string]Encoder{}

// Register makes e the encoder for a two-letter language code. It is meant to be called from a language package's init and panics if the code is already taken.
func Register(code string, e Encoder) {
	if _, ok := registry[code]; ok {
		panic(fmt.Sprintf("phonetic: encoder for %q registered twice", code))
	}

	registry[code] = e
}

// For returns the encoder for a language or locale code ("de", "de-DE", "en_GB"), falling back to English, or nil when neither is registered.
func For(code string) Encoder {
	code = strings.ToLower(code)
	if i := strings.IndexAny(code, "-_"); i >= 0 {
		code = code[:i]
	}

	if e, ok := registry[code]; ok {
		return e
	}

	return registry[fallback]
}

// IsDigits reports whether word is non-empty and made only of ASCII digits; every encoder passes such a word through as its own key.
func IsDigits(word string) bool {
	if word == "" {
		return false
	}

	for i := range len(word) {
		if word[i] < '0' || word[i] > '9' {
			return false
		}
	}

	return true
}
