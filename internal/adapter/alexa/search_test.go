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
	testCases := []struct {
		name     string
		intent   string
		have     string
		expected string
	}{
		{name: "ShouldSearchEverythingInMovies", intent: "GlobalSearch", have: "hot fuzz", expected: "ShowView movies title hot fuzz"},
		{name: "ShouldSearchMovies", intent: "SearchMovies", have: "dune", expected: "ShowView movies title dune"},
		{name: "ShouldSearchTVShows", intent: "SearchTVShows", have: "severance", expected: "ShowView tvshows title severance"},
		{name: "ShouldSearchMusicForArtist", intent: "SearchMusic", have: "queen", expected: "ShowView artists title queen"},
		{name: "ShouldSearchMusicForAlbum", intent: "SearchMusic", have: "innuendo", expected: "ShowView albums title innuendo"},
		{name: "ShouldSearchMoviesForTitleThatIsAlsoAShow", intent: "SearchMovies", have: "severance", expected: "ShowView movies title severance"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			c := searchConversation(t, "en-AU")

			resp := c.say(tc.intent, map[string]slotSpec{"Query": {value: tc.have}})
			assert.Equal(t, "Searching for "+tc.have+".", speechOf(resp))
			assert.Equal(t, []string{tc.expected}, c.backend.Calls())
		})
	}
}

func TestSearchAsksForTheFilmOrTheShow(t *testing.T) {
	testCases := []struct {
		name   string
		locale string
		show   string
		film   string
	}{
		{name: "ShouldAskInEnAU", locale: "en-AU", show: "show", film: "film"},
		{name: "ShouldAskInDeDE", locale: "de-DE", show: "sendung", film: "film"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			c := searchConversation(t, tc.locale)

			resp := c.say("GlobalSearch", map[string]slotSpec{"Query": {value: "severance"}})
			if tc.locale == "en-AU" {
				assert.Equal(t, "Do you want the film or the TV show Severance?", speechOf(resp))
			}

			require.NotNil(t, resp.Response.ShouldEndSession)
			assert.False(t, *resp.Response.ShouldEndSession)
			assert.Empty(t, c.backend.Calls(), "nothing opens before the answer")

			c.say("ChooseMatch", map[string]slotSpec{"Which": {value: tc.show}})
			assert.Equal(t, []string{"ShowView tvshows title severance"}, c.backend.Calls(), "the show")

			c = searchConversation(t, tc.locale)
			c.say("GlobalSearch", map[string]slotSpec{"Query": {value: "severance"}})
			c.say("ChooseMatch", map[string]slotSpec{"Which": {value: tc.film}})
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
