package alexa

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/testkit"
)

// conversation sends requests from the linked Echo in one locale, handing each the session attributes the previous response kept, as Alexa does within a session.
type conversation struct {
	t       *testing.T
	h       *Handler
	backend *testkit.Backend
	locale  string
	attrs   map[string]any
}

func newConversation(t *testing.T, locale string) *conversation {
	t.Helper()

	h, backend, _ := newHandler(t)

	return &conversation{t: t, h: h, backend: backend, locale: locale}
}

// say sends an intent and returns the response, keeping its session attributes for the next turn.
func (c *conversation) say(name string, slots map[string]slotSpec) ResponseEnvelope {
	c.t.Helper()

	var env RequestEnvelope
	require.NoError(c.t, json.Unmarshal(intentRequest(c.t, name, slots), &env))
	env.Request.Locale = c.locale
	env.Session.Attributes = c.attrs

	body, err := json.Marshal(env)
	require.NoError(c.t, err)

	var resp ResponseEnvelope
	require.NoError(c.t, json.Unmarshal(serveBody(c.t, c.h, body), &resp))
	c.attrs = resp.SessionAttributes

	return resp
}

func speechOf(resp ResponseEnvelope) string {
	if resp.Response.OutputSpeech == nil {
		return ""
	}

	return resp.Response.OutputSpeech.Text
}

func repromptOf(resp ResponseEnvelope) string {
	if resp.Response.Reprompt == nil {
		return ""
	}

	return resp.Response.Reprompt.OutputSpeech.Text
}

// TestTitleYearInEveryLanguage checks a "did you mean" question names each title's year in the listener's language, not in English.
func TestTitleYearInEveryLanguage(t *testing.T) {
	tests := map[string]string{
		"en-AU": "Did you mean Dune from 1984 or Dune from 2021?",
		"de-DE": "Meintest du Dune von 1984 oder Dune von 2021?",
		"es-ES": "¿Te refieres a Dune de 1984 o a Dune de 2021?",
		"fr-FR": "Vouliez-vous dire Dune de 1984 ou Dune de 2021 ?",
		"it-IT": "Intendevi Dune del 1984 o Dune del 2021?",
		"nl-NL": "Bedoel je Dune uit 1984 of Dune uit 2021?",
		"pt-BR": "Você quis dizer Dune de 1984 ou Dune de 2021?",
		"ar-SA": "هل المقصود Dune لعام 1984 أم Dune لعام 2021؟",
		"hi-IN": "क्या आपका मतलब 1984 वाली Dune था या 2021 वाली Dune?",
		"ja-JP": "1984年のDuneと2021年のDuneのどちらですか？",
	}

	for locale, want := range tests {
		t.Run(locale, func(t *testing.T) {
			resp := newConversation(t, locale).say("WatchMovie", map[string]slotSpec{"Movie": {value: "dune"}})

			assert.Equal(t, want, speechOf(resp))
			assert.Equal(t, want, repromptOf(resp))
		})
	}
}

// TestTitleYearConfirm checks a yes/no question about one title names its year in the listener's language too.
func TestTitleYearConfirm(t *testing.T) {
	tests := map[string]string{
		"en-AU": "Did you mean Hot Fuzz from 2007?",
		"de-DE": "Meintest du Hot Fuzz von 2007?",
		"ja-JP": "2007年のHot Fuzzのことですか？",
	}

	for locale, want := range tests {
		t.Run(locale, func(t *testing.T) {
			resp := newConversation(t, locale).say("WatchMovie", map[string]slotSpec{"Movie": {value: "fuzz"}})

			assert.Equal(t, want, speechOf(resp))
		})
	}
}
