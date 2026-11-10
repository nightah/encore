package alexa

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/core/media"
)

// searchConversation is a conversation whose library also has a film named as the sample TV show Severance is.
func searchConversation(t *testing.T, locale string) *conversation {
	t.Helper()

	h, backend, _, _ := newRoomHandlerWith(t, media.Item{Kind: media.KindMovie, ID: 5, Title: "Severance", Year: 2006})

	return &conversation{t: t, h: h, backend: backend, locale: locale}
}

func TestSearchIntents(t *testing.T) {
	tests := []struct {
		intent, query, view string
	}{
		{"GlobalSearch", "hot fuzz", "ShowView movies title hot fuzz"},
		{"SearchMovies", "dune", "ShowView movies title dune"},
		{"SearchTVShows", "severance", "ShowView tvshows title severance"},
		{"SearchMusic", "queen", "ShowView artists title queen"},
		{"SearchMusic", "innuendo", "ShowView albums title innuendo"},
		{"SearchMovies", "severance", "ShowView movies title severance"},
	}

	for _, tt := range tests {
		t.Run(tt.intent+" "+tt.query, func(t *testing.T) {
			c := searchConversation(t, "en-AU")

			resp := c.say(tt.intent, map[string]slotSpec{"Query": {value: tt.query}})
			assert.Equal(t, "Searching for "+tt.query+".", speechOf(resp))
			assert.Equal(t, []string{tt.view}, c.backend.Calls())
		})
	}
}

func TestSearchAsksForTheFilmOrTheShow(t *testing.T) {
	for locale, answers := range map[string][2]string{"en-AU": {"show", "film"}, "de-DE": {"sendung", "film"}} {
		t.Run(locale, func(t *testing.T) {
			c := searchConversation(t, locale)

			resp := c.say("GlobalSearch", map[string]slotSpec{"Query": {value: "severance"}})
			if locale == "en-AU" {
				assert.Equal(t, "Do you want the film or the TV show Severance?", speechOf(resp))
			}

			require.NotNil(t, resp.Response.ShouldEndSession)
			assert.False(t, *resp.Response.ShouldEndSession)
			assert.Empty(t, c.backend.Calls(), "nothing opens before the answer")

			c.say("ChooseMatch", map[string]slotSpec{"Which": {value: answers[0]}})
			assert.Equal(t, []string{"ShowView tvshows title severance"}, c.backend.Calls(), "the show")

			c = searchConversation(t, locale)
			c.say("GlobalSearch", map[string]slotSpec{"Query": {value: "severance"}})
			c.say("ChooseMatch", map[string]slotSpec{"Which": {value: answers[1]}})
			assert.Equal(t, []string{"ShowView movies title severance"}, c.backend.Calls(), "the film")
		})
	}
}

func TestSearchQuestionNeedsAChoice(t *testing.T) {
	c := searchConversation(t, "en-AU")
	c.say("GlobalSearch", map[string]slotSpec{"Query": {value: "severance"}})

	resp := c.say("AMAZON.YesIntent", nil)
	assert.Equal(t, "There's more than one. Do you want the film or the TV show Severance?", speechOf(resp), "yes chooses neither")

	resp = c.say("ChooseMatch", map[string]slotSpec{"Answer": {value: "severance"}})
	assert.Equal(t, "There's more than one. Do you want the film or the TV show Severance?", speechOf(resp), "both are called that")
	assert.Empty(t, c.backend.Calls())

	c.say("ChooseMatch", map[string]slotSpec{"Which": {value: "second", id: "second"}})
	assert.Equal(t, []string{"ShowView tvshows title severance"}, c.backend.Calls())
}
