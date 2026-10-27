package i18n

import (
	"io/fs"
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
