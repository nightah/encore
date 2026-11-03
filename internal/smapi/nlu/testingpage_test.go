package nlu_test

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/smapi/nlu"
)

// TestTestingPageListsPendingRows keeps the testers' page listing exactly the pending rows, so testers try what Amazon still misreads and nothing that now passes.
func TestTestingPageListsPendingRows(t *testing.T) {
	page, err := os.ReadFile("../../../docs/src/content/docs/contributing/testing.md")
	require.NoError(t, err)

	fixtures, err := nlu.Load(os.DirFS("../../../testdata/nlu"))
	require.NoError(t, err)

	var want []string

	for _, f := range fixtures {
		for _, c := range f.Cases {
			if c.Pending != "" {
				want = append(want, f.Locale+" "+c.Utterance)
			}
		}
	}

	assert.ElementsMatch(t, want, listedPhrases(string(page)))
}

var localeHeading = regexp.MustCompile("^### .*\\(`([a-z]{2}-[A-Z]{2})`\\)$")

// listedPhrases is each table row's first cell under the page's locale headings, as "locale phrase".
func listedPhrases(page string) []string {
	var (
		locale string
		rows   []string
	)

	for line := range strings.Lines(page) {
		line = strings.TrimSpace(line)

		if m := localeHeading.FindStringSubmatch(line); m != nil {
			locale = m[1]

			continue
		}

		if locale == "" || !strings.HasPrefix(line, "| ") || strings.HasPrefix(line, "| Say ") || strings.HasPrefix(line, "| ---") {
			continue
		}

		phrase, _, _ := strings.Cut(strings.TrimPrefix(line, "| "), " |")
		rows = append(rows, locale+" "+phrase)
	}

	return rows
}
