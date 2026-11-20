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

	testCases := []struct {
		name           string
		locale         string
		expectedEncore string
		expectedKodi   string
	}{
		{name: "ShouldOpenInEnAU", locale: "en-AU", expectedEncore: "open encore", expectedKodi: "open kodi"},
		{name: "ShouldOpenInEnGB", locale: "en-GB", expectedEncore: "open encore", expectedKodi: "open kodi"},
		{name: "ShouldOpenInDeDE", locale: "de-DE", expectedEncore: "öffne encore", expectedKodi: "öffne kodi"},
		{name: "ShouldOpenInEsMX", locale: "es-MX", expectedEncore: "abre encore", expectedKodi: "abre kodi"},
		{name: "ShouldOpenInFrFR", locale: "fr-FR", expectedEncore: "ouvre kodi", expectedKodi: "ouvre kodi"},
		{name: "ShouldOpenInFrCA", locale: "fr-CA", expectedEncore: "ouvre kodi", expectedKodi: "ouvre kodi"},
		{name: "ShouldOpenInItIT", locale: "it-IT", expectedEncore: "apri encore", expectedKodi: "apri kodi"},
		{name: "ShouldOpenInArSA", locale: "ar-SA", expectedEncore: "افتحي انكور", expectedKodi: "افتحي كودي"},
		{name: "ShouldOpenInHiIN", locale: "hi-IN", expectedEncore: "encore शुरू करो", expectedKodi: "kodi शुरू करो"},
		{name: "ShouldOpenInJaJP", locale: "ja-JP", expectedEncore: "アンコールを開いて", expectedKodi: "コディを開いて"},
		{name: "ShouldOpenInNlNL", locale: "nl-NL", expectedEncore: "open encore", expectedKodi: "open kodi"},
		{name: "ShouldOpenInPtBR", locale: "pt-BR", expectedEncore: "abrir kodi", expectedKodi: "abrir kodi"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expectedEncore, src.OpenPhrase(tc.locale, ""))
			assert.Equal(t, tc.expectedKodi, src.OpenPhrase(tc.locale, InvocationKodi))
		})
	}

	for _, locale := range src.LocaleNames() {
		assert.NotEmpty(t, src.OpenPhrase(locale, ""), locale)
		assert.NotEmpty(t, src.Locales[locale].Launch.Skill, locale)
	}

	assert.Empty(t, src.OpenPhrase("xx-XX", ""))
}
