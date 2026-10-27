package skillpkg

import (
	"fmt"
	"strings"
)

// examplePhrases per language; each must match a sample utterance. %s is the invocation name.
var examplePhrases = map[string][]string{
	"en": {"Alexa, open %s", "Alexa, ask %s to pause the movie", "Alexa, tell %s to turn the volume up"},
}

func localePublishing(name, language, invocation string) LocalePublishing {
	lp := LocalePublishing{
		Name: name, Summary: summary, Description: description, Keywords: []string{"kodi", "media center", "remote"},
		SmallIconURI: smallIconURI, LargeIconURI: largeIconURI,
	}

	for _, p := range examplePhrases[language] {
		lp.ExamplePhrases = append(lp.ExamplePhrases, fmt.Sprintf(p, invocation))
	}

	return lp
}

// country is "" for a locale without a two-letter region, such as "de".
func country(locale string) string {
	_, region, ok := strings.Cut(locale, "-")
	if !ok || len(region) != 2 {
		return ""
	}

	return strings.ToUpper(region)
}
