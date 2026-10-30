package i18n

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

// crowdinFiles are the files in lang/en that Crowdin translates (.crowdin.yml).
var crowdinFiles = []string{"responses.yaml", "utterances.yaml", "types.yaml"}

type value struct {
	path  string
	style yaml.Style
}

// values lists a mapping's scalar values depth first, in file order.
func values(node *yaml.Node, prefix string) []value {
	if node.Kind == yaml.DocumentNode {
		return values(node.Content[0], prefix)
	}

	var out []value

	for i := 0; i+1 < len(node.Content); i += 2 {
		path := prefix + node.Content[i].Value
		if v := node.Content[i+1]; v.Kind == yaml.MappingNode {
			out = append(out, values(v, path+".")...)
		} else {
			out = append(out, value{path, v.Style})
		}
	}

	return out
}

// header is the comment lines a file starts with.
func header(b []byte) []string {
	var lines []string

	for line := range strings.SplitSeq(string(b), "\n") {
		if !strings.HasPrefix(line, "#") {
			break
		}

		lines = append(lines, line)
	}

	return lines
}

// Crowdin writes a translation from the English file: its comments, its key order and each value's style (double-quoted or a |- block), whatever the repository had. Keeping the translated files that way means a sync changes only what was translated. yamllint (.yamllint.yml) checks the rest: double quotes and no blank lines.
func TestTranslationsUseTheEnglishLayout(t *testing.T) {
	langs, err := fs.ReadDir(FS, "lang")
	require.NoError(t, err)

	for _, file := range crowdinFiles {
		enBytes, err := fs.ReadFile(FS, "lang/en/"+file)
		require.NoError(t, err)

		var en yaml.Node
		require.NoError(t, yaml.Unmarshal(enBytes, &en))

		enStyle := map[string]yaml.Style{}
		var enOrder []string

		for _, v := range values(&en, "") {
			enStyle[v.path] = v.style
			enOrder = append(enOrder, v.path)
		}

		for _, lang := range langs {
			if lang.Name() == "en" {
				continue
			}

			path := "lang/" + lang.Name() + "/" + file
			b, err := fs.ReadFile(FS, path)
			require.NoError(t, err)

			var tr yaml.Node
			require.NoError(t, yaml.Unmarshal(b, &tr), path)

			assert.Equal(t, header(enBytes), header(b), "%s starts with the English file's comment", path)

			present := map[string]bool{}
			var order []string

			for _, v := range values(&tr, "") {
				present[v.path] = true
				order = append(order, v.path)

				want, ok := enStyle[v.path]
				if assert.True(t, ok, "%s: %s isn't in the English file", path, v.path) {
					assert.Equal(t, want, v.style, "%s: %s is written in a different style from the English (%s)", path, v.path, styleName(want))
				}
			}

			var enPresent []string
			for _, p := range enOrder {
				if present[p] {
					enPresent = append(enPresent, p)
				}
			}

			assert.Equal(t, enPresent, order, "%s keeps the English file's order", path)
		}
	}
}

// Every locale's language must ship each source file, and every language must be used by a locale, or the binary builds models from a partial language.
func TestEmbeddedLocalesAndLanguagesMatch(t *testing.T) {
	_, err := fs.Stat(FS, "model.yaml")
	require.NoError(t, err, "the language-neutral model is embedded")

	locales, err := fs.ReadDir(FS, "locales")
	require.NoError(t, err)
	require.NotEmpty(t, locales)

	used := map[string]bool{}

	for _, l := range locales {
		name := strings.TrimSuffix(l.Name(), ".yaml")
		assert.Regexp(t, `^[a-z]{2}-[A-Z]{2}$`, name, "%s is named after its locale", l.Name())

		b, err := fs.ReadFile(FS, "locales/"+l.Name())
		require.NoError(t, err)

		var loc struct {
			Language   string `yaml:"language"`
			Invocation string `yaml:"invocation"`
		}
		require.NoError(t, yaml.Unmarshal(b, &loc), l.Name())

		assert.NotEmpty(t, loc.Invocation, "%s has an invocation name", l.Name())
		assert.True(t, strings.HasPrefix(name, loc.Language+"-"), "%s belongs to its language %q", l.Name(), loc.Language)

		used[loc.Language] = true

		for _, file := range append([]string{"examples.yaml"}, crowdinFiles...) {
			_, err := fs.Stat(FS, "lang/"+loc.Language+"/"+file)
			require.NoError(t, err, "%s needs lang/%s/%s", l.Name(), loc.Language, file)
		}
	}

	langs, err := fs.ReadDir(FS, "lang")
	require.NoError(t, err)

	for _, lang := range langs {
		assert.True(t, used[lang.Name()], "no locale uses lang/%s", lang.Name())
	}
}

// es-US carries es-MX's overlay (see the comment in each file), so a change to one must reach the other.
func TestUSSpanishFollowsMexicanSpanish(t *testing.T) {
	overlay := func(name string) map[string]any {
		b, err := fs.ReadFile(FS, "locales/"+name+".yaml")
		require.NoError(t, err)

		var out map[string]any
		require.NoError(t, yaml.Unmarshal(b, &out), name)

		return out
	}

	assert.Equal(t, overlay("es-MX"), overlay("es-US"))
}

// es-MX and es-US replace the responses lang/es words with the Peninsular perfect ("no he encontrado") by the preterite Latin American speakers use ("no encontré"), as their headers say. With "todavía no" the perfect is usual in the Americas too.
func TestAmericanSpanishSaysThePreterite(t *testing.T) {
	read := func(name string, v any) {
		b, err := fs.ReadFile(FS, name)
		require.NoError(t, err)
		require.NoError(t, yaml.Unmarshal(b, v), name)
	}

	var spain map[string]string

	read("lang/es/responses.yaml", &spain)

	perfect := regexp.MustCompile(`(?i)\b(he|has|ha|hemos|han) \p{L}+(ado|ido)\b`)

	for _, locale := range []string{"es-MX", "es-US"} {
		var overlay struct {
			Responses map[string]string `yaml:"responses"`
		}

		read("locales/"+locale+".yaml", &overlay)

		for key, text := range spain {
			if own, ok := overlay.Responses[key]; ok {
				text = own
			}

			if strings.HasPrefix(text, "Todavía no") {
				continue
			}

			assert.NotRegexp(t, perfect, text, "%s %s", locale, key)
		}
	}
}

func styleName(s yaml.Style) string {
	switch s {
	case yaml.DoubleQuotedStyle:
		return "double-quoted"
	case yaml.LiteralStyle:
		return "a |- block"
	default:
		return "plain"
	}
}
