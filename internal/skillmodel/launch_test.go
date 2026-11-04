package skillmodel

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/i18n"
)

func TestLocaleInvocationName(t *testing.T) {
	ja := Locale{Invocation: "アンコール", KodiInvocation: "コディ"}
	en := Locale{Invocation: "encore"}

	assert.Equal(t, "アンコール", ja.InvocationName(""))
	assert.Equal(t, "コディ", ja.InvocationName(InvocationKodi))
	assert.Equal(t, "kodi", en.InvocationName(InvocationKodi), "a Latin locale writes kodi as it is")
	assert.Equal(t, "lounge remote", en.InvocationName("lounge remote"))
}

func TestLaunchPhrases(t *testing.T) {
	l := Launch{Open: "open {invocation}", Also: []string{"start {invocation}"}, Skill: "open the {invocation} skill"}
	assert.Equal(t, []string{"open encore", "start encore", "open the encore skill"}, l.Phrases("encore"))
	assert.Empty(t, Launch{}.Phrases("encore"))
	assert.Equal(t, "アンコールを開いて", Say("{invocation}を開いて", "アンコール"))
}

// TestShippedOpenPhrases checks every shipped locale opens the skill with one of Amazon's launch phrases for its language, under both invocation choices.
func TestShippedOpenPhrases(t *testing.T) {
	src, err := Load(i18n.FS)
	require.NoError(t, err)

	for locale, want := range map[string][2]string{
		"en-AU": {"open encore", "open kodi"},
		"en-GB": {"open encore", "open kodi"},
		"de-DE": {"öffne encore", "öffne kodi"},
		"es-MX": {"abre encore", "abre kodi"},
		"fr-FR": {"ouvre kodi", "ouvre kodi"},
		"fr-CA": {"ouvre kodi", "ouvre kodi"},
		"it-IT": {"apri encore", "apri kodi"},
		"ar-SA": {"افتحي انكور", "افتحي كودي"},
		"hi-IN": {"encore शुरू करो", "kodi शुरू करो"},
		"ja-JP": {"アンコールを開いて", "コディを開いて"},
		"nl-NL": {"open encore", "open kodi"},
		"pt-BR": {"abrir kodi", "abrir kodi"},
	} {
		assert.Equal(t, want[0], src.OpenPhrase(locale, ""), locale)
		assert.Equal(t, want[1], src.OpenPhrase(locale, InvocationKodi), locale)
	}

	for _, locale := range src.LocaleNames() {
		assert.NotEmpty(t, src.OpenPhrase(locale, ""), locale)
		assert.NotEmpty(t, src.Locales[locale].Launch.Skill, locale)
	}

	assert.Empty(t, src.OpenPhrase("xx-XX", ""))
}
