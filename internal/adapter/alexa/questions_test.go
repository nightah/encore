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

func pendingOf(t *testing.T, resp ResponseEnvelope) []string {
	t.Helper()

	var titles []string

	for _, c := range pendingChoices(resp.SessionAttributes) {
		titles = append(titles, c.DocID)
	}

	return titles
}

// TestUnrecognisedReplyAsksAgain checks an answer Alexa couldn't place keeps the question and its choices, in every language.
func TestUnrecognisedReplyAsksAgain(t *testing.T) {
	tests := map[string]string{
		"en-AU": "Sorry, I didn't catch that. Did you mean Dune from 1984 or Dune from 2021?",
		"de-DE": "Tut mir leid, das habe ich nicht verstanden. Meintest du Dune von 1984 oder Dune von 2021?",
		"es-ES": "Lo siento, no te he entendido. ¿Te refieres a Dune de 1984 o a Dune de 2021?",
		"fr-FR": "Désolée, je n'ai pas compris. Vouliez-vous dire Dune de 1984 ou Dune de 2021 ?",
		"it-IT": "Scusa, non ho capito. Intendevi Dune del 1984 o Dune del 2021?",
		"nl-NL": "Sorry, dat verstond ik niet. Bedoel je Dune uit 1984 of Dune uit 2021?",
		"pt-BR": "Desculpe, não entendi. Você quis dizer Dune de 1984 ou Dune de 2021?",
		"ar-SA": "عذرا، لم أفهم ذلك. هل المقصود Dune لعام 1984 أم Dune لعام 2021؟",
		"hi-IN": "माफ़ कीजिए, मैं समझ नहीं पाई। क्या आपका मतलब 1984 वाली Dune था या 2021 वाली Dune?",
		"ja-JP": "すみません、聞き取れませんでした。1984年のDuneと2021年のDuneのどちらですか？",
	}

	for locale, want := range tests {
		t.Run(locale, func(t *testing.T) {
			c := newConversation(t, locale)
			question := c.say("WatchMovie", map[string]slotSpec{"Movie": {value: "dune"}})

			for _, intent := range []string{"AMAZON.FallbackIntent", "AMAZON.FallbackIntent", "AMAZON.NavigateHomeIntent"} {
				resp := c.say(intent, nil)

				assert.Equal(t, want, speechOf(resp), intent)
				assert.Equal(t, speechOf(question), repromptOf(resp), "the reprompt is the question itself")
				assert.Equal(t, []string{"movie/1", "movie/2"}, pendingOf(t, resp), "the choices survive")
				require.NotNil(t, resp.Response.ShouldEndSession)
				assert.False(t, *resp.Response.ShouldEndSession)
			}
		})
	}
}

// TestUnrecognisedReplyAsksAgainForAnyQuestion checks a recommendation and a power confirmation are asked again too, keeping what their answer does.
func TestUnrecognisedReplyAsksAgainForAnyQuestion(t *testing.T) {
	c := newConversation(t, "en-AU")
	c.say("Power", map[string]slotSpec{"PowerAction": {value: "shut down", id: "shutdown"}})

	resp := c.say("AMAZON.FallbackIntent", nil)
	assert.Equal(t, "Sorry, I didn't catch that. Are you sure you want to shut down Lounge?", speechOf(resp))

	c.say("AMAZON.YesIntent", nil)
	assert.Equal(t, []string{"System shutdown"}, c.backend.Calls(), "yes still confirms")

	c = newConversation(t, "en-AU")
	recommendation := speechOf(c.say("Recommend", map[string]slotSpec{"MediaType": {value: "movie", id: "movie"}}))

	resp = c.say("AMAZON.FallbackIntent", nil)
	assert.Equal(t, "Sorry, I didn't catch that. "+recommendation, speechOf(resp))
	assert.NotNil(t, sessionFollowupOf(resp.SessionAttributes).Recommend, "no still asks for another")
}

// TestUnrecognisedReplyWithoutQuestion checks an unrecognised request outside a question is the usual fallback.
func TestUnrecognisedReplyWithoutQuestion(t *testing.T) {
	c := newConversation(t, "en-AU")
	c.say("WatchMovie", map[string]slotSpec{"Movie": {value: "hot fuzz"}})

	resp := c.say("AMAZON.FallbackIntent", nil)
	assert.Equal(t, "Sorry, I didn't catch that. Try: play the movie Dune.", speechOf(resp))
	assert.Nil(t, resp.SessionAttributes)
}

// TestYesToEitherOrAsksAgain checks "yes" to "A or B?" asks which rather than playing A, in every language.
func TestYesToEitherOrAsksAgain(t *testing.T) {
	tests := map[string]string{
		"en-AU": "There's more than one. Did you mean Dune from 1984 or Dune from 2021?",
		"de-DE": "Es gibt mehrere. Meintest du Dune von 1984 oder Dune von 2021?",
		"es-ES": "Hay varios. ¿Te refieres a Dune de 1984 o a Dune de 2021?",
		"fr-FR": "Il y en a plusieurs. Vouliez-vous dire Dune de 1984 ou Dune de 2021 ?",
		"it-IT": "Ce n'è più di uno. Intendevi Dune del 1984 o Dune del 2021?",
		"nl-NL": "Er zijn er meerdere. Bedoel je Dune uit 1984 of Dune uit 2021?",
		"pt-BR": "Tem mais de um. Você quis dizer Dune de 1984 ou Dune de 2021?",
		"ar-SA": "فيه أكثر من واحد. هل المقصود Dune لعام 1984 أم Dune لعام 2021؟",
		"hi-IN": "एक से ज़्यादा हैं। क्या आपका मतलब 1984 वाली Dune था या 2021 वाली Dune?",
		"ja-JP": "候補が複数あります。1984年のDuneと2021年のDuneのどちらですか？",
	}

	for locale, want := range tests {
		t.Run(locale, func(t *testing.T) {
			c := newConversation(t, locale)
			question := c.say("WatchMovie", map[string]slotSpec{"Movie": {value: "dune"}})

			resp := c.say("AMAZON.YesIntent", nil)

			assert.Equal(t, want, speechOf(resp))
			assert.Equal(t, speechOf(question), repromptOf(resp))
			assert.Equal(t, []string{"movie/1", "movie/2"}, pendingOf(t, resp))
			assert.Empty(t, c.backend.Calls(), "nothing plays")
		})
	}
}

// TestYesToOneChoicePlays checks "yes" still plays the title a yes/no question offered.
func TestYesToOneChoicePlays(t *testing.T) {
	c := newConversation(t, "en-AU")
	c.say("WatchMovie", map[string]slotSpec{"Movie": {value: "fuzz"}})

	resp := c.say("AMAZON.YesIntent", nil)

	assert.Equal(t, "Playing Hot Fuzz.", speechOf(resp))
	assert.Equal(t, []string{"Play movie/4 resume"}, c.backend.Calls())
}

// TestNoOffersOtherMatches checks "no" to a "did you mean" question offers the next matches, then says there are no more, in every language.
func TestNoOffersOtherMatches(t *testing.T) {
	tests := map[string][2]string{
		"en-AU": {"Did you mean Dune: Part Two from 2024?", "I couldn't find any other matches."},
		"de-DE": {"Meintest du Dune: Part Two von 2024?", "Ich habe keine weiteren Treffer gefunden."},
		"es-ES": {"¿Te refieres a Dune: Part Two de 2024?", "No he encontrado más coincidencias."},
		"fr-FR": {"Vouliez-vous dire Dune: Part Two de 2024 ?", "Je n'ai pas trouvé d'autres résultats."},
		"it-IT": {"Intendevi Dune: Part Two del 2024?", "Non ho trovato altri risultati."},
		"nl-NL": {"Bedoel je Dune: Part Two uit 2024?", "Ik kan geen andere titels vinden."},
		"pt-BR": {"Você quis dizer Dune: Part Two de 2024?", "Não encontrei outros resultados."},
		"ar-SA": {"هل المقصود Dune: Part Two لعام 2024؟", "لم أجد نتائج أخرى."},
		"hi-IN": {"क्या आपका मतलब 2024 वाली Dune: Part Two था?", "मुझे और कोई टाइटल नहीं मिला।"},
		"ja-JP": {"2024年のDune: Part Twoのことですか？", "ほかに候補は見つかりませんでした。"},
	}

	for locale, want := range tests {
		t.Run(locale, func(t *testing.T) {
			c := newConversation(t, locale)
			c.say("WatchMovie", map[string]slotSpec{"Movie": {value: "dun"}})

			next := c.say("AMAZON.NoIntent", nil)
			assert.Equal(t, want[0], speechOf(next))
			assert.Equal(t, []string{"movie/3"}, pendingOf(t, next), "the next match is offered")
			require.NotNil(t, next.Response.ShouldEndSession)
			assert.False(t, *next.Response.ShouldEndSession)

			last := c.say("AMAZON.NoIntent", nil)
			assert.Equal(t, want[1], speechOf(last))
			require.NotNil(t, last.Response.ShouldEndSession)
			assert.True(t, *last.Response.ShouldEndSession)
			assert.Empty(t, c.backend.Calls())
		})
	}
}

// TestYesAfterNoPlaysTheNextMatch checks the match "no" moved on to can be chosen.
func TestYesAfterNoPlaysTheNextMatch(t *testing.T) {
	c := newConversation(t, "en-AU")
	c.say("WatchMovie", map[string]slotSpec{"Movie": {value: "dun"}})
	c.say("AMAZON.NoIntent", nil)

	resp := c.say("AMAZON.YesIntent", nil)

	assert.Equal(t, "Playing Dune: Part Two.", speechOf(resp))
	assert.Equal(t, []string{"Play movie/3 resume"}, c.backend.Calls())
}

// TestNoWithoutOtherMatches checks "no" to a question with nothing else to offer says so and ends.
func TestNoWithoutOtherMatches(t *testing.T) {
	c := newConversation(t, "en-AU")
	c.say("WatchMovie", map[string]slotSpec{"Movie": {value: "dune"}})

	resp := c.say("AMAZON.NoIntent", nil)

	assert.Equal(t, "I couldn't find any other matches.", speechOf(resp))
	assert.Nil(t, resp.SessionAttributes)
}

// TestChooseByPosition checks "the second one" and "the older one" choose between the titles offered, as each language says them.
func TestChooseByPosition(t *testing.T) {
	tests := map[string][2]string{
		"en-AU": {"second", "older"},
		"de-DE": {"zweite", "ältere"},
		"es-ES": {"segundo", "más antiguo"},
		"fr-FR": {"deuxième", "plus ancien"},
		"it-IT": {"secondo", "più vecchio"},
		"nl-NL": {"tweede", "oudere"},
		"pt-BR": {"segunda", "mais antigo"},
		"ar-SA": {"الثاني", "الأقدم"},
		"hi-IN": {"दूसरा", "पुराना"},
		"ja-JP": {"二つ目の方", "古い方"},
	}

	for locale, heard := range tests {
		t.Run(locale, func(t *testing.T) {
			c := newConversation(t, locale)
			c.say("WatchMovie", map[string]slotSpec{"Movie": {value: "dune"}})
			c.say("ChooseMatch", map[string]slotSpec{"Which": {value: heard[0]}})
			assert.Equal(t, []string{"Play movie/2 resume"}, c.backend.Calls(), "the second title offered")

			c = newConversation(t, locale)
			c.say("WatchMovie", map[string]slotSpec{"Movie": {value: "dune"}})
			c.say("ChooseMatch", map[string]slotSpec{"Which": {value: heard[1]}})
			assert.Equal(t, []string{"Play movie/1 resume"}, c.backend.Calls(), "the older title offered")
		})
	}
}

func TestChooseByEveryPosition(t *testing.T) {
	tests := map[string]string{"first": "movie/1", "second": "movie/2", "last": "movie/2", "newer": "movie/2", "older": "movie/1"}

	for id, want := range tests {
		t.Run(id, func(t *testing.T) {
			c := newConversation(t, "en-AU")
			c.say("WatchMovie", map[string]slotSpec{"Movie": {value: "dune"}})

			resp := c.say("ChooseMatch", map[string]slotSpec{"Which": {value: id, id: id}})

			assert.Equal(t, "Playing Dune.", speechOf(resp))
			assert.Equal(t, []string{"Play " + want + " resume"}, c.backend.Calls())
		})
	}
}

// TestChoosePositionNotOffered checks an answer that names no title offered asks again.
func TestChoosePositionNotOffered(t *testing.T) {
	c := newConversation(t, "en-AU")
	c.say("WatchMovie", map[string]slotSpec{"Movie": {value: "fuzz"}})

	for _, which := range []string{"second", "newer", "older", ""} {
		resp := c.say("ChooseMatch", map[string]slotSpec{"Which": {value: which, id: which}})

		assert.Equal(t, "There's more than one. Did you mean Hot Fuzz from 2007?", speechOf(resp), which)
		assert.Equal(t, []string{"movie/4"}, pendingOf(t, resp))
	}

	assert.Empty(t, c.backend.Calls())

	c.say("ChooseMatch", map[string]slotSpec{"Which": {value: "first", id: "first"}})
	assert.Equal(t, []string{"Play movie/4 resume"}, c.backend.Calls())
}

// TestChooseByTitle checks a bare title answers the question: one of the titles found, a title the question found but hadn't offered yet, or a new search.
func TestChooseByTitle(t *testing.T) {
	tests := map[string]struct {
		answer string
		calls  []string
		speech string
	}{
		"offered":     {answer: "dune 2021", calls: []string{"Play movie/2 resume"}, speech: "Playing Dune."},
		"not offered": {answer: "dune part two", calls: []string{"Play movie/3 resume"}, speech: "Playing Dune: Part Two."},
		"another":     {answer: "hot fuzz", calls: []string{"Play movie/4 resume"}, speech: "Playing Hot Fuzz."},
		"several":     {answer: "dune", speech: "There's more than one. Did you mean Dune from 1984 or Dune from 2021?"},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			c := newConversation(t, "en-AU")
			c.say("WatchMovie", map[string]slotSpec{"Movie": {value: "dun"}})

			resp := c.say("ChooseMatch", map[string]slotSpec{"Answer": {value: tc.answer}})

			assert.Equal(t, tc.speech, speechOf(resp))
			assert.Equal(t, tc.calls, c.backend.Calls())
		})
	}
}

// TestChooseOutsideQuestion checks an answer with no question waiting is not acted on, and that a one-word command Alexa heard as an answer still runs.
func TestChooseOutsideQuestion(t *testing.T) {
	c := newConversation(t, "en-AU")

	resp := c.say("ChooseMatch", map[string]slotSpec{"Which": {value: "first", id: "first"}})
	assert.Equal(t, "Sorry, I didn't catch that. Try: play the movie Dune.", speechOf(resp))

	resp = c.say("ChooseMatch", map[string]slotSpec{"Answer": {value: "dune"}})
	assert.Equal(t, "Sorry, I didn't catch that. Try: play the movie Dune.", speechOf(resp))
	assert.Empty(t, c.backend.Calls(), "a bare title outside a question plays nothing")

	c.say("ChooseMatch", map[string]slotSpec{"Answer": {value: "select"}})
	assert.Equal(t, []string{"Input select"}, c.backend.Calls(), "\"select\" is Navigate's")

	c = newConversation(t, "de-DE")
	c.say("ChooseMatch", map[string]slotSpec{"Answer": {value: "runter"}})
	assert.Equal(t, []string{"Input down"}, c.backend.Calls(), "in the listener's language")

	c = newConversation(t, "en-AU")
	c.say("Power", map[string]slotSpec{"PowerAction": {value: "shut down", id: "shutdown"}})

	resp = c.say("ChooseMatch", map[string]slotSpec{"Which": {value: "first", id: "first"}})
	assert.Equal(t, "Sorry, I didn't catch that. Are you sure you want to shut down Lounge?", speechOf(resp), "a question without choices is asked again")
	assert.Empty(t, c.backend.Calls())
}

// TestChooseAfterNo checks the titles "no" moved on to can be chosen by position.
func TestChooseAfterNo(t *testing.T) {
	c := newConversation(t, "en-AU")
	c.say("WatchMovie", map[string]slotSpec{"Movie": {value: "dun"}})
	c.say("AMAZON.NoIntent", nil)

	c.say("ChooseMatch", map[string]slotSpec{"Which": {value: "first", id: "first"}})

	assert.Equal(t, []string{"Play movie/3 resume"}, c.backend.Calls())
}

// TestChooseWithoutStoredQuestion checks choices kept without their question are asked about with the usual fallback.
func TestChooseWithoutStoredQuestion(t *testing.T) {
	c := newConversation(t, "en-AU")
	c.say("WatchMovie", map[string]slotSpec{"Movie": {value: "fuzz"}})
	delete(c.attrs, sessionQuestion)

	resp := c.say("ChooseMatch", map[string]slotSpec{"Which": {value: "second", id: "second"}})

	assert.Equal(t, "There's more than one. Sorry, I didn't catch that. Try: play the movie Dune.", speechOf(resp))
	assert.Equal(t, []string{"movie/4"}, pendingOf(t, resp))
}

// TestHelpExplainsTheQuestion checks help during a question says how to answer it and asks it again, keeping its choices, in every language.
func TestHelpExplainsTheQuestion(t *testing.T) {
	tests := map[string][2]string{
		"en-AU": {"You can say the first one, the second one, the newer one or the title, or no for other matches. ", "Please answer yes or no. "},
		"de-DE": {"Du kannst sagen: den ersten, den zweiten, den neueren oder den Titel, oder nein für andere Treffer. ", "Bitte antworte mit ja oder nein. "},
		"es-ES": {"Puedes decir el primero, el segundo, el más reciente o el título, o no para ver otras coincidencias. ", "Responde sí o no, por favor. "},
		"fr-FR": {"Vous pouvez dire le premier, le deuxième, le plus récent ou le titre, ou non pour d'autres résultats. ", "Répondez par oui ou par non. "},
		"it-IT": {"Puoi dire il primo, il secondo, il più recente o il titolo, oppure no per altri risultati. ", "Rispondi sì o no, per favore. "},
		"nl-NL": {"Je kunt zeggen: de eerste, de tweede, de nieuwere of de titel, of nee voor andere resultaten. ", "Antwoord alsjeblieft met ja of nee. "},
		"pt-BR": {"Você pode dizer o primeiro, o segundo, o mais recente ou o título, ou não para outros resultados. ", "Responda sim ou não, por favor. "},
		"ar-SA": {"يمكن قول: الأول، أو الثاني، أو الأحدث، أو العنوان، أو لا لنتائج أخرى. ", "الرجاء الإجابة بنعم أو لا. "},
		"hi-IN": {"आप कह सकते हैं: पहला वाला, दूसरा वाला, नया वाला या टाइटल का नाम, या दूसरे नतीजों के लिए नहीं। ", "कृपया हाँ या नहीं में जवाब दीजिए। "},
		"ja-JP": {"「一つ目」「二つ目」「新しい方」やタイトルで答えてください。ほかの候補は「いいえ」と言ってください。", "「はい」か「いいえ」で答えてください。"},
	}

	for locale, want := range tests {
		t.Run(locale, func(t *testing.T) {
			c := newConversation(t, locale)
			question := c.say("WatchMovie", map[string]slotSpec{"Movie": {value: "dune"}})

			resp := c.say("AMAZON.HelpIntent", nil)
			assert.Equal(t, want[0]+speechOf(question), speechOf(resp))
			assert.Equal(t, speechOf(question), repromptOf(resp))
			assert.Equal(t, []string{"movie/1", "movie/2"}, pendingOf(t, resp))

			c = newConversation(t, locale)
			question = c.say("WatchMovie", map[string]slotSpec{"Movie": {value: "fuzz"}})

			resp = c.say("AMAZON.HelpIntent", nil)
			assert.Equal(t, want[1]+speechOf(question), speechOf(resp))
			assert.Equal(t, []string{"movie/4"}, pendingOf(t, resp))
		})
	}
}

// TestHelpWithoutQuestion checks help outside a question is the general help.
func TestHelpWithoutQuestion(t *testing.T) {
	resp := newConversation(t, "en-AU").say("AMAZON.HelpIntent", nil)

	assert.Equal(t, "You can say things like: play the movie Dune, play the next episode of a show, or what's playing.", speechOf(resp))
	assert.Nil(t, resp.SessionAttributes)
}
