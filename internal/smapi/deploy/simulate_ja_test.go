package deploy_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/i18n"
	"github.com/nightah/encore/internal/skillmodel"
	"github.com/nightah/encore/internal/smapi/deploy"
	"github.com/nightah/encore/internal/smapi/skillpkg"
)

// TestOpenPhraseJapaneseSkill checks the phrase the smoke test opens a ja-JP skill with, built from the shipped sources under both invocation choices: Amazon's Japanese launch phrase "<name>を開いて" with the katakana name.
func TestOpenPhraseJapaneseSkill(t *testing.T) {
	src, err := skillmodel.Load(i18n.FS)
	require.NoError(t, err)

	no, yes := false, true
	privacy := skillpkg.Privacy{AllowsPurchases: &no, UsesPersonalInfo: &no, IsChildDirected: &no, IsExportCompliant: &yes, ContainsAds: &no}

	for invocation, want := range map[string]string{"": "アンコールを開いて", skillmodel.InvocationKodi: "コディを開いて"} {
		pkg, err := skillpkg.Build(src, skillpkg.Options{
			Locales:    []string{"ja-JP"},
			Invocation: invocation,
			Endpoint:   skillpkg.Endpoint{URI: "https://encore.example.com/api/v1/alexa"},
			Privacy:    privacy,
		})
		require.NoError(t, err)

		assert.Equal(t, want, deploy.OpenPhrase(src, "ja-JP", pkg.Invocation("ja-JP")), invocation)
	}
}
