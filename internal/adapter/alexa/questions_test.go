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
	testCases := []struct {
		name     string
		locale   string
		expected string
	}{
		{name: "ShouldNameEachTitlesYearInEnAU", locale: "en-AU", expected: "Did you mean Dune from 1984 or Dune from 2021?"},
		{name: "ShouldNameEachTitlesYearInDeDE", locale: "de-DE", expected: "Meintest du Dune von 1984 oder Dune von 2021?"},
		{name: "ShouldNameEachTitlesYearInEsES", locale: "es-ES", expected: "¿Te refieres a Dune de 1984 o a Dune de 2021?"},
		{name: "ShouldNameEachTitlesYearInFrFR", locale: "fr-FR", expected: "Vouliez-vous dire Dune de 1984 ou Dune de 2021 ?"},
		{name: "ShouldNameEachTitlesYearInItIT", locale: "it-IT", expected: "Intendevi Dune del 1984 o Dune del 2021?"},
		{name: "ShouldNameEachTitlesYearInNlNL", locale: "nl-NL", expected: "Bedoel je Dune uit 1984 of Dune uit 2021?"},
		{name: "ShouldNameEachTitlesYearInPtBR", locale: "pt-BR", expected: "Você quis dizer Dune de 1984 ou Dune de 2021?"},
		{name: "ShouldNameEachTitlesYearInArSA", locale: "ar-SA", expected: "هل المقصود Dune لعام 1984 أم Dune لعام 2021؟"},
		{name: "ShouldNameEachTitlesYearInHiIN", locale: "hi-IN", expected: "क्या आपका मतलब Dune (1984) था या Dune (2021)?"},
		{name: "ShouldNameEachTitlesYearInJaJP", locale: "ja-JP", expected: "1984年のDuneと2021年のDuneのどちらですか？"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			resp := newConversation(t, tc.locale).say("WatchMovie", map[string]slotSpec{"Movie": {value: "dune"}})

			assert.Equal(t, tc.expected, speechOf(resp))
			assert.Equal(t, tc.expected, repromptOf(resp))
		})
	}
}

// TestTitleYearConfirm checks a yes/no question about one title names its year in the listener's language too.
func TestTitleYearConfirm(t *testing.T) {
	testCases := []struct {
		name     string
		locale   string
		expected string
	}{
		{name: "ShouldNameTheTitlesYearInEnAU", locale: "en-AU", expected: "Did you mean Hot Fuzz from 2007?"},
		{name: "ShouldNameTheTitlesYearInDeDE", locale: "de-DE", expected: "Meintest du Hot Fuzz von 2007?"},
		{name: "ShouldNameTheTitlesYearInJaJP", locale: "ja-JP", expected: "2007年のHot Fuzzのことですか？"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			resp := newConversation(t, tc.locale).say("WatchMovie", map[string]slotSpec{"Movie": {value: "fuzz"}})

			assert.Equal(t, tc.expected, speechOf(resp))
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
	testCases := []struct {
		name     string
		locale   string
		expected string
	}{
		{name: "ShouldAskAgainInEnAU", locale: "en-AU", expected: "Sorry, I didn't catch that. Did you mean Dune from 1984 or Dune from 2021?"},
		{name: "ShouldAskAgainInDeDE", locale: "de-DE", expected: "Tut mir leid, das habe ich nicht verstanden. Meintest du Dune von 1984 oder Dune von 2021?"},
		{name: "ShouldAskAgainInEsES", locale: "es-ES", expected: "Lo siento, no te he entendido. ¿Te refieres a Dune de 1984 o a Dune de 2021?"},
		{name: "ShouldAskAgainInFrFR", locale: "fr-FR", expected: "Désolée, je n'ai pas compris. Vouliez-vous dire Dune de 1984 ou Dune de 2021 ?"},
		{name: "ShouldAskAgainInItIT", locale: "it-IT", expected: "Scusa, non ho capito. Intendevi Dune del 1984 o Dune del 2021?"},
		{name: "ShouldAskAgainInNlNL", locale: "nl-NL", expected: "Sorry, dat verstond ik niet. Bedoel je Dune uit 1984 of Dune uit 2021?"},
		{name: "ShouldAskAgainInPtBR", locale: "pt-BR", expected: "Desculpe, não entendi. Você quis dizer Dune de 1984 ou Dune de 2021?"},
		{name: "ShouldAskAgainInArSA", locale: "ar-SA", expected: "عذرا، لم أفهم ذلك. هل المقصود Dune لعام 1984 أم Dune لعام 2021؟"},
		{name: "ShouldAskAgainInHiIN", locale: "hi-IN", expected: "माफ़ कीजिए, मैं समझ नहीं पाई। क्या आपका मतलब Dune (1984) था या Dune (2021)?"},
		{name: "ShouldAskAgainInJaJP", locale: "ja-JP", expected: "すみません、聞き取れませんでした。1984年のDuneと2021年のDuneのどちらですか？"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			c := newConversation(t, tc.locale)
			question := c.say("WatchMovie", map[string]slotSpec{"Movie": {value: "dune"}})

			for _, intent := range []string{"AMAZON.FallbackIntent", "AMAZON.FallbackIntent", "AMAZON.NavigateHomeIntent"} {
				resp := c.say(intent, nil)

				assert.Equal(t, tc.expected, speechOf(resp), intent)
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
	testCases := []struct {
		name     string
		locale   string
		expected string
	}{
		{name: "ShouldAskWhichInEnAU", locale: "en-AU", expected: "There's more than one. Did you mean Dune from 1984 or Dune from 2021?"},
		{name: "ShouldAskWhichInDeDE", locale: "de-DE", expected: "Es gibt mehrere. Meintest du Dune von 1984 oder Dune von 2021?"},
		{name: "ShouldAskWhichInEsES", locale: "es-ES", expected: "Hay varios. ¿Te refieres a Dune de 1984 o a Dune de 2021?"},
		{name: "ShouldAskWhichInFrFR", locale: "fr-FR", expected: "Il y en a plusieurs. Vouliez-vous dire Dune de 1984 ou Dune de 2021 ?"},
		{name: "ShouldAskWhichInItIT", locale: "it-IT", expected: "Ce n'è più di uno. Intendevi Dune del 1984 o Dune del 2021?"},
		{name: "ShouldAskWhichInNlNL", locale: "nl-NL", expected: "Er zijn er meerdere. Bedoel je Dune uit 1984 of Dune uit 2021?"},
		{name: "ShouldAskWhichInPtBR", locale: "pt-BR", expected: "Tem mais de um. Você quis dizer Dune de 1984 ou Dune de 2021?"},
		{name: "ShouldAskWhichInArSA", locale: "ar-SA", expected: "فيه أكثر من واحد. هل المقصود Dune لعام 1984 أم Dune لعام 2021؟"},
		{name: "ShouldAskWhichInHiIN", locale: "hi-IN", expected: "एक से ज़्यादा हैं। क्या आपका मतलब Dune (1984) था या Dune (2021)?"},
		{name: "ShouldAskWhichInJaJP", locale: "ja-JP", expected: "候補が複数あります。1984年のDuneと2021年のDuneのどちらですか？"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			c := newConversation(t, tc.locale)
			question := c.say("WatchMovie", map[string]slotSpec{"Movie": {value: "dune"}})

			resp := c.say("AMAZON.YesIntent", nil)

			assert.Equal(t, tc.expected, speechOf(resp))
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
	testCases := []struct {
		name   string
		locale string
		next   string
		last   string
	}{
		{name: "ShouldOfferOtherMatchesInEnAU", locale: "en-AU", next: "Did you mean Dune: Part Two from 2024?", last: "I couldn't find any other matches."},
		{name: "ShouldOfferOtherMatchesInDeDE", locale: "de-DE", next: "Meintest du Dune: Part Two von 2024?", last: "Ich habe keine weiteren Treffer gefunden."},
		{name: "ShouldOfferOtherMatchesInEsES", locale: "es-ES", next: "¿Te refieres a Dune: Part Two de 2024?", last: "No he encontrado más coincidencias."},
		{name: "ShouldOfferOtherMatchesInFrFR", locale: "fr-FR", next: "Vouliez-vous dire Dune: Part Two de 2024 ?", last: "Je n'ai pas trouvé d'autres résultats."},
		{name: "ShouldOfferOtherMatchesInItIT", locale: "it-IT", next: "Intendevi Dune: Part Two del 2024?", last: "Non ho trovato altri risultati."},
		{name: "ShouldOfferOtherMatchesInNlNL", locale: "nl-NL", next: "Bedoel je Dune: Part Two uit 2024?", last: "Ik kan geen andere titels vinden."},
		{name: "ShouldOfferOtherMatchesInPtBR", locale: "pt-BR", next: "Você quis dizer Dune: Part Two de 2024?", last: "Não encontrei outros resultados."},
		{name: "ShouldOfferOtherMatchesInArSA", locale: "ar-SA", next: "هل المقصود Dune: Part Two لعام 2024؟", last: "لم أجد نتائج أخرى."},
		{name: "ShouldOfferOtherMatchesInHiIN", locale: "hi-IN", next: "क्या आपका मतलब Dune: Part Two (2024) था?", last: "मुझे और कोई टाइटल नहीं मिला।"},
		{name: "ShouldOfferOtherMatchesInJaJP", locale: "ja-JP", next: "2024年のDune: Part Twoのことですか？", last: "ほかに候補は見つかりませんでした。"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			c := newConversation(t, tc.locale)
			c.say("WatchMovie", map[string]slotSpec{"Movie": {value: "dun"}})

			next := c.say("AMAZON.NoIntent", nil)
			assert.Equal(t, tc.next, speechOf(next))
			assert.Equal(t, []string{"movie/3"}, pendingOf(t, next), "the next match is offered")
			require.NotNil(t, next.Response.ShouldEndSession)
			assert.False(t, *next.Response.ShouldEndSession)

			last := c.say("AMAZON.NoIntent", nil)
			assert.Equal(t, tc.last, speechOf(last))
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
	testCases := []struct {
		name   string
		locale string
		second string
		older  string
	}{
		{name: "ShouldChooseByPositionInEnAU", locale: "en-AU", second: "second", older: "older"},
		{name: "ShouldChooseByPositionInDeDE", locale: "de-DE", second: "zweite", older: "ältere"},
		{name: "ShouldChooseByPositionInEsES", locale: "es-ES", second: "segundo", older: "más antiguo"},
		{name: "ShouldChooseByPositionInFrFR", locale: "fr-FR", second: "deuxième", older: "plus ancien"},
		{name: "ShouldChooseByPositionInItIT", locale: "it-IT", second: "secondo", older: "più vecchio"},
		{name: "ShouldChooseByPositionInNlNL", locale: "nl-NL", second: "tweede", older: "oudere"},
		{name: "ShouldChooseByPositionInPtBR", locale: "pt-BR", second: "segunda", older: "mais antigo"},
		{name: "ShouldChooseByPositionInArSA", locale: "ar-SA", second: "الثاني", older: "الأقدم"},
		{name: "ShouldChooseByPositionInHiIN", locale: "hi-IN", second: "दूसरा", older: "पुराना"},
		{name: "ShouldChooseByPositionInJaJP", locale: "ja-JP", second: "二つ目の方", older: "古い方"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			c := newConversation(t, tc.locale)
			c.say("WatchMovie", map[string]slotSpec{"Movie": {value: "dune"}})
			c.say("ChooseMatch", map[string]slotSpec{"Which": {value: tc.second}})
			assert.Equal(t, []string{"Play movie/2 resume"}, c.backend.Calls(), "the second title offered")

			c = newConversation(t, tc.locale)
			c.say("WatchMovie", map[string]slotSpec{"Movie": {value: "dune"}})
			c.say("ChooseMatch", map[string]slotSpec{"Which": {value: tc.older}})
			assert.Equal(t, []string{"Play movie/1 resume"}, c.backend.Calls(), "the older title offered")
		})
	}
}

func TestChooseByEveryPosition(t *testing.T) {
	testCases := []struct {
		name     string
		have     string
		expected string
	}{
		{name: "ShouldChooseFirst", have: "first", expected: "movie/1"},
		{name: "ShouldChooseSecond", have: "second", expected: "movie/2"},
		{name: "ShouldChooseLast", have: "last", expected: "movie/2"},
		{name: "ShouldChooseNewer", have: "newer", expected: "movie/2"},
		{name: "ShouldChooseOlder", have: "older", expected: "movie/1"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			c := newConversation(t, "en-AU")
			c.say("WatchMovie", map[string]slotSpec{"Movie": {value: "dune"}})

			resp := c.say("ChooseMatch", map[string]slotSpec{"Which": {value: tc.have, id: tc.have}})

			assert.Equal(t, "Playing Dune.", speechOf(resp))
			assert.Equal(t, []string{"Play " + tc.expected + " resume"}, c.backend.Calls())
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
	testCases := []struct {
		name   string
		answer string
		calls  []string
		speech string
	}{
		{name: "ShouldPlayATitleOffered", answer: "dune 2021", calls: []string{"Play movie/2 resume"}, speech: "Playing Dune."},
		{name: "ShouldPlayATitleFoundButNotOffered", answer: "dune part two", calls: []string{"Play movie/3 resume"}, speech: "Playing Dune: Part Two."},
		{name: "ShouldPlayAnotherTitle", answer: "hot fuzz", calls: []string{"Play movie/4 resume"}, speech: "Playing Hot Fuzz."},
		{name: "ShouldAskAgainForSeveralTitles", answer: "dune", speech: "There's more than one. Did you mean Dune from 1984 or Dune from 2021?"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
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
	testCases := []struct {
		name   string
		locale string
		choose string
		yesNo  string
	}{
		{name: "ShouldExplainTheQuestionInEnAU", locale: "en-AU", choose: "You can say the first one, the second one, the newer one or the title, or no for other matches. ", yesNo: "Please answer yes or no. "},
		{name: "ShouldExplainTheQuestionInDeDE", locale: "de-DE", choose: "Du kannst sagen: den ersten, den zweiten, den neueren oder den Titel, oder nein für andere Treffer. ", yesNo: "Bitte antworte mit ja oder nein. "},
		{name: "ShouldExplainTheQuestionInEsES", locale: "es-ES", choose: "Puedes decir el primero, el segundo, el más reciente o el título, o no para ver otras coincidencias. ", yesNo: "Responde sí o no, por favor. "},
		{name: "ShouldExplainTheQuestionInFrFR", locale: "fr-FR", choose: "Vous pouvez dire le premier, le deuxième, le plus récent ou le titre, ou non pour d'autres résultats. ", yesNo: "Répondez par oui ou par non. "},
		{name: "ShouldExplainTheQuestionInItIT", locale: "it-IT", choose: "Puoi dire il primo, il secondo, il più recente o il titolo, oppure no per altri risultati. ", yesNo: "Rispondi sì o no, per favore. "},
		{name: "ShouldExplainTheQuestionInNlNL", locale: "nl-NL", choose: "Je kunt zeggen: de eerste, de tweede, de nieuwere of de titel, of nee voor andere resultaten. ", yesNo: "Antwoord alsjeblieft met ja of nee. "},
		{name: "ShouldExplainTheQuestionInPtBR", locale: "pt-BR", choose: "Você pode dizer o primeiro, o segundo, o mais recente ou o título, ou não para outros resultados. ", yesNo: "Responda sim ou não, por favor. "},
		{name: "ShouldExplainTheQuestionInArSA", locale: "ar-SA", choose: "يمكن قول: الأول، أو الثاني، أو الأحدث، أو العنوان، أو لا لنتائج أخرى. ", yesNo: "الرجاء الإجابة بنعم أو لا. "},
		{name: "ShouldExplainTheQuestionInHiIN", locale: "hi-IN", choose: "आप कह सकते हैं: पहला वाला, दूसरा वाला, नया वाला या टाइटल का नाम, या दूसरे नतीजों के लिए नहीं। ", yesNo: "कृपया हाँ या नहीं में जवाब दीजिए। "},
		{name: "ShouldExplainTheQuestionInJaJP", locale: "ja-JP", choose: "「一つ目」「二つ目」「新しい方」やタイトルで答えてください。ほかの候補は「いいえ」と言ってください。", yesNo: "「はい」か「いいえ」で答えてください。"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			c := newConversation(t, tc.locale)
			question := c.say("WatchMovie", map[string]slotSpec{"Movie": {value: "dune"}})

			resp := c.say("AMAZON.HelpIntent", nil)
			assert.Equal(t, tc.choose+speechOf(question), speechOf(resp))
			assert.Equal(t, speechOf(question), repromptOf(resp))
			assert.Equal(t, []string{"movie/1", "movie/2"}, pendingOf(t, resp))

			c = newConversation(t, tc.locale)
			question = c.say("WatchMovie", map[string]slotSpec{"Movie": {value: "fuzz"}})

			resp = c.say("AMAZON.HelpIntent", nil)
			assert.Equal(t, tc.yesNo+speechOf(question), speechOf(resp))
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

// TestAnswerWordsAreNotTitles checks that while a question's titles wait, an answer Alexa fills with one of a built-in intent's words is that intent, in every language: Alexa puts whatever was said in the answer it elicits, and on an Echo "help" played The Help and "yes" asked about Yes Man.
func TestAnswerWordsAreNotTitles(t *testing.T) {
	testCases := []struct {
		name   string
		locale string
		have   string
		intent string
	}{
		{name: "ShouldReadHelpAsHelpInEnAU", locale: "en-AU", have: "help", intent: "AMAZON.HelpIntent"},
		{name: "ShouldReadCapitalisedYesAsYesInEnAU", locale: "en-AU", have: "Yes", intent: "AMAZON.YesIntent"},
		{name: "ShouldReadYeahAsYesInEnAU", locale: "en-AU", have: "yeah", intent: "AMAZON.YesIntent"},
		{name: "ShouldReadNoAsNoInEnAU", locale: "en-AU", have: "no", intent: "AMAZON.NoIntent"},
		{name: "ShouldReadStopAsStopInEnAU", locale: "en-AU", have: "stop", intent: "AMAZON.StopIntent"},
		{name: "ShouldReadNeverMindAsCancelInEnAU", locale: "en-AU", have: "never mind", intent: "AMAZON.CancelIntent"},
		{name: "ShouldReadHilfeAsHelpInDeDE", locale: "de-DE", have: "hilfe", intent: "AMAZON.HelpIntent"},
		{name: "ShouldReadSiAsYesInEsUS", locale: "es-US", have: "si", intent: "AMAZON.YesIntent"},
		{name: "ShouldReadNonAsNoInFrFR", locale: "fr-FR", have: "non", intent: "AMAZON.NoIntent"},
		{name: "ShouldReadHaiAsYesInJaJP", locale: "ja-JP", have: "はい", intent: "AMAZON.YesIntent"},
		{name: "ShouldReadMadadAsHelpInHiIN", locale: "hi-IN", have: "मदद", intent: "AMAZON.HelpIntent"},
		{name: "ShouldReadLaAsNoInArSA", locale: "ar-SA", have: "لا", intent: "AMAZON.NoIntent"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			heard := newConversation(t, tc.locale)
			heard.say("WatchMovie", map[string]slotSpec{"Movie": {value: "dune"}})
			got := heard.say(chooseIntent, map[string]slotSpec{answerSlot: {value: tc.have}})

			said := newConversation(t, tc.locale)
			said.say("WatchMovie", map[string]slotSpec{"Movie": {value: "dune"}})
			want := said.say(tc.intent, nil)

			assert.Equal(t, speechOf(want), speechOf(got))
			assert.Equal(t, said.backend.Calls(), heard.backend.Calls())
			assert.False(t, containsPrefix(heard.backend.Calls(), "Play"), "nothing plays: %q", heard.backend.Calls())
		})
	}
}

// TestStopEndsAQuestion checks that "stop" while a question waits ends the question, as cancel does, rather than stopping playback that may not be there: on an Echo it answered "Nothing is playing".
func TestStopEndsAQuestion(t *testing.T) {
	c := newConversation(t, "en-AU")
	c.say("WatchMovie", map[string]slotSpec{"Movie": {value: "dune"}})

	resp := c.say("AMAZON.StopIntent", nil)

	assert.Equal(t, "Okay, never mind.", speechOf(resp))
	assert.Empty(t, c.backend.Calls())
	require.NotNil(t, resp.Response.ShouldEndSession)
	assert.True(t, *resp.Response.ShouldEndSession)
}

// TestContinueWatchingTheLastShow checks that a next-episode request for "the last show", as Alexa heard "continue watching the last show" on an Echo, continues the last show rather than asking about Last Light or The Last Dance.
func TestContinueWatchingTheLastShow(t *testing.T) {
	testCases := []struct {
		name string
		have string
	}{
		{name: "ShouldContinueForTheLastShow", have: "the last show"},
		{name: "ShouldContinueForTheLastShowInAnyCase", have: "The Last Show"},
		{name: "ShouldContinueForLastShowWithoutArticle", have: "last show"},
		{name: "ShouldContinueForWatchingTheLastShow", have: "watching the last show"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			heard := newConversation(t, "en-AU")
			got := heard.say("WatchNextEpisode", map[string]slotSpec{"Show": {value: tc.have}})

			said := newConversation(t, "en-AU")
			want := said.say("ContinueWatching", nil)

			assert.Equal(t, speechOf(want), speechOf(got))
			assert.Equal(t, said.backend.Calls(), heard.backend.Calls())
		})
	}

	c := newConversation(t, "en-AU")
	c.say("WatchNextEpisode", map[string]slotSpec{"Show": {value: "the last kingdom"}})
	assert.NotContains(t, c.backend.Calls(), "NowPlaying", "a show called something else is still looked for")
}

// TestLastShow checks that only a next-episode request for "the last show" is read as continue watching, and that it keeps its room.
func TestLastShow(t *testing.T) {
	speech := shippedSpeech(t)
	room := Slot{Name: "Room", Value: "bedroom"}

	got := speech.LastShow("en-AU", Intent{Name: "WatchNextEpisode", Slots: map[string]Slot{"Show": {Name: "Show", Value: "the last show"}, "Room": room}})
	assert.Equal(t, Intent{Name: "ContinueWatching", Slots: map[string]Slot{"Room": room}}, got)

	testCases := []struct {
		name string
		have Intent
	}{
		{name: "ShouldKeepANextEpisodeOfAnotherShow", have: Intent{Name: "WatchNextEpisode", Slots: map[string]Slot{"Show": {Name: "Show", Value: "the last kingdom"}}}},
		{name: "ShouldKeepAMovieCalledTheLastShow", have: Intent{Name: "WatchMovie", Slots: map[string]Slot{"Movie": {Name: "Movie", Value: "the last show"}}}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.have, speech.LastShow("en-AU", tc.have))
		})
	}
}
