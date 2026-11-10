package actions

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/core/match"
	"github.com/nightah/encore/internal/core/media"
	"github.com/nightah/encore/internal/testkit"
)

func TestPowerAsksFirst(t *testing.T) {
	target, backend, _ := newTarget()
	svc := newService()

	o := svc.Power(t.Context(), target, media.SystemReboot)
	assert.Equal(t, "power.confirm_reboot", o.Say.ID)
	assert.False(t, o.EndSession)
	require.NotNil(t, o.Followup)
	assert.Equal(t, media.SystemReboot, o.Followup.Confirm)
	assert.Empty(t, backend.Calls(), "nothing happens until the user says yes")

	o = svc.ConfirmPower(t.Context(), target, media.SystemReboot)
	assert.Equal(t, "power.reboot", o.Say.ID)
	assert.Equal(t, []string{"System reboot"}, backend.Calls())
}

func TestShutdownQuits(t *testing.T) {
	target, backend, _ := newTarget()
	target.ShutdownQuits = true
	svc := newService()

	assert.Equal(t, "power.confirm_quit", svc.Power(t.Context(), target, media.SystemShutdown).Say.ID)
	assert.Equal(t, "power.quit", svc.ConfirmPower(t.Context(), target, media.SystemShutdown).Say.ID)
	assert.Equal(t, []string{"System quit"}, backend.Calls())
}

func TestEjectAndLibrary(t *testing.T) {
	target, backend, _ := newTarget()
	svc := newService()

	assert.Equal(t, "power.eject", svc.Eject(t.Context(), target).Say.ID)
	assert.Equal(t, "library.scan_both", svc.UpdateLibrary(t.Context(), target, false).Say.ID)
	assert.Equal(t, "library.clean_music", svc.UpdateLibrary(t.Context(), target, true, media.DatabaseMusic).Say.ID)
	assert.Equal(t, []string{"System eject", "ScanLibrary video", "ScanLibrary music", "CleanLibrary music"}, backend.Calls())
}

func TestAddons(t *testing.T) {
	target, backend, _ := newTarget()
	svc := newService()

	backend.SetAddons(media.Addon{ID: "plugin.video.youtube", Name: "YouTube"}, media.Addon{ID: "script.globalsearch", Name: "Global Search"})

	o := svc.LaunchAddon(t.Context(), target, "you tube")
	assert.Equal(t, "addon.opening", o.Say.ID)
	assert.Equal(t, "YouTube", o.Say.Params["addon"])

	assert.Equal(t, "addon.not_found", svc.LaunchAddon(t.Context(), target, "netflix").Say.ID)

	target.SearchMode = media.SearchModeGlobal
	o = svc.GlobalSearch(t.Context(), target, "dune", nil)
	assert.Equal(t, "addon.searching", o.Say.ID)

	assert.Contains(t, backend.Calls(), "ExecuteAddon plugin.video.youtube")
	assert.Contains(t, backend.Calls(), "ExecuteAddon script.globalsearch searchstring=dune")
}

func TestAddonNamesMustBeSpoken(t *testing.T) {
	target, backend, _ := newTarget()
	withAddons(backend)
	svc := newService()

	assert.Equal(t, "remote.no_text", svc.GlobalSearch(t.Context(), target, " \t", nil).Say.ID)
	assert.Equal(t, "addon.not_found", svc.LaunchAddon(t.Context(), target, "").Say.ID)
	assert.Equal(t, []string{"Addons"}, backend.Calls(), "a blank search asks Kodi nothing")
}

func TestUpdateLibraryStopsAtTheFirstFailure(t *testing.T) {
	target, backend, _ := newTarget()
	target, f := failing(target, backend, errBoom, "ScanLibrary")

	assertFailed(t, newService().UpdateLibrary(t.Context(), target, false, media.DatabaseVideo, media.DatabaseMusic))
	assert.Equal(t, []string{"ScanLibrary"}, f.failed, "the music library isn't scanned after the video library failed")

	target, _, _ = newTarget()
	o := newService().UpdateLibrary(t.Context(), target, false, media.DatabaseVideo)
	assert.Equal(t, "library.scan_video", o.Say.ID)
}

func TestLibrarySearchShowsTheBestKind(t *testing.T) {
	for query, view := range map[string]string{
		"hot fuzz":          "ShowView movies title hot fuzz",
		"severance":         "ShowView tvshows title severance",
		"queen":             "ShowView artists title queen",
		"innuendo":          "ShowView albums title innuendo",
		"bohemian rhapsody": "ShowView musicvideos title bohemian rhapsody",
		"fuzz":              "ShowView movies title fuzz",
		"dune":              "ShowView movies title dune",
	} {
		target, backend, _ := newTarget()

		o := newService().GlobalSearch(t.Context(), target, query, nil)
		assert.Equal(t, "addon.searching", o.Say.ID, query)
		assert.Equal(t, query, o.Say.Params["query"])
		assert.Empty(t, o.Note, "the library is the default, not a fallback")
		require.NotNil(t, o.Match, "the match is logged")
		assert.Equal(t, []string{view}, backend.Calls(), query)
	}
}

func TestLibrarySearchTieBreaks(t *testing.T) {
	soundtrack := media.Item{Kind: media.KindAlbum, ID: 43, Title: "Hot Fuzz", Artist: "Various"}
	theColony := media.Item{Kind: media.KindMovie, ID: 6, Title: "Colony"}
	colony := media.Item{Kind: media.KindTVShow, ID: 21, Title: "Colony"}

	target, backend, _ := newTarget(append(testkit.SampleLibrary(), soundtrack)...)
	newService().GlobalSearch(t.Context(), target, "hot fuzz", nil)
	assert.Equal(t, []string{"ShowView movies title hot fuzz"}, backend.Calls(), "a film beats its soundtrack")

	target, backend, _ = newTarget(soundtrack, testkit.SampleLibrary()[3])
	newService().GlobalSearch(t.Context(), target, "hot fuzz", []media.Kind{media.KindAlbum})
	assert.Equal(t, []string{"ShowView albums title hot fuzz"}, backend.Calls(), "unless music was asked for")

	// "The Colony" scores as "Colony" does in the real matcher; the exact title wins the tie.
	target, backend, _ = newTarget(colony, theColony)
	target.Matcher = matcherFunc(func(match.Query) match.Result {
		return match.Result{Normalized: "colony", Decision: match.DecisionAsk, Candidates: []match.Candidate{
			{Doc: match.Doc{ID: "movie/6", Kind: media.KindMovie, Ref: theColony.Ref(), Title: "The Colony"}, Score: 1},
			{Doc: match.Doc{ID: "tvshow/21", Kind: media.KindTVShow, Ref: colony.Ref(), Title: "Colony"}, Score: 1},
		}}
	})
	newService().GlobalSearch(t.Context(), target, "colony", nil)
	assert.Equal(t, []string{"ShowView tvshows title colony"}, backend.Calls())
}

func TestLibrarySearchAsksBetweenAFilmAndAShow(t *testing.T) {
	film := media.Item{Kind: media.KindMovie, ID: 5, Title: "Severance", Year: 2006}
	target, backend, _ := newTarget(append(testkit.SampleLibrary(), film)...)
	svc := newService()

	o := svc.GlobalSearch(t.Context(), target, "severance", nil)
	assert.Equal(t, "search.film_or_show", o.Say.ID)
	assert.Equal(t, "Severance", o.Say.Params["title"])
	assert.False(t, o.EndSession)
	require.NotNil(t, o.Reprompt)
	require.Len(t, o.Pending, 2)
	assert.Equal(t, media.KindMovie, o.Pending[0].Ref.Kind)
	assert.Equal(t, media.KindTVShow, o.Pending[1].Ref.Kind)
	require.NotNil(t, o.Followup)
	assert.Equal(t, &SearchRequest{Query: "severance"}, o.Followup.Search)
	assert.Empty(t, backend.Calls(), "nothing opens until it's answered")

	for which, view := range map[string]string{AnswerShow: "ShowView tvshows title severance", AnswerFilm: "ShowView movies title severance", AnswerFirst: "ShowView movies title severance"} {
		target, backend, _ := newTarget(append(testkit.SampleLibrary(), film)...)

		o := svc.ChooseSearch(t.Context(), target, Answer{Which: which}, o.Pending, *o.Followup.Search)
		assert.Equal(t, "addon.searching", o.Say.ID, which)
		assert.Equal(t, []string{view}, backend.Calls(), which)
	}

	for _, a := range []Answer{{Title: "severance"}, {}, {Which: AnswerSecond + "x"}} {
		o := svc.ChooseSearch(t.Context(), target, a, o.Pending, *o.Followup.Search)
		assert.Equal(t, "match.which_one", o.Say.ID, "%+v names neither", a)
		assert.True(t, o.AskAgain)
	}

	target, backend, _ = newTarget(append(testkit.SampleLibrary(), film)...)
	o = svc.GlobalSearch(t.Context(), target, "severance", []media.Kind{media.KindTVShow})
	assert.Equal(t, []string{"ShowView tvshows title severance"}, backend.Calls(), "\"search for the show\" doesn't ask")
}

func TestChooseSearchByTitle(t *testing.T) {
	target, backend, _ := newTarget()
	pending := []Choice{{Ref: media.Ref{Kind: media.KindMovie, ID: 4}, Title: "Hot Fuzz"}, {Ref: media.Ref{Kind: media.KindTVShow, ID: 10}, Title: "Severance"}}

	o := newService().ChooseSearch(t.Context(), target, Answer{Title: "severance"}, pending, SearchRequest{Query: "severance"})
	assert.Equal(t, "addon.searching", o.Say.ID)
	assert.Equal(t, []string{"ShowView tvshows title severance"}, backend.Calls())
}

func TestLibrarySearchFiltersByWhatsInTheLibrary(t *testing.T) {
	bastard := media.Item{Kind: media.KindTVShow, ID: 22, Title: "The Bastard Son"}

	// The matcher doesn't know the show, but the library does: a word that's only part of a title still shows it.
	target, backend, _ := newTarget()
	target.Library = &testkit.Library{Cached: append(testkit.SampleLibrary(), bastard)}
	o := newService().GlobalSearch(t.Context(), target, "bastard", nil)
	assert.Equal(t, "addon.searching", o.Say.ID)
	assert.Equal(t, []string{"ShowView tvshows title bastard"}, backend.Calls())

	// A misheard query no title contains filters by the title the answer chose instead.
	target, backend, _ = newTarget()
	target.Library = &testkit.Library{Cached: testkit.SampleLibrary()}
	pending := []Choice{{Ref: media.Ref{Kind: media.KindTVShow, ID: 10}, Title: "Severance"}}
	newService().ChooseSearch(t.Context(), target, Answer{Which: AnswerFirst}, pending, SearchRequest{Query: "severence"})
	assert.Equal(t, []string{"ShowView tvshows title Severance"}, backend.Calls())

	target, backend, _ = newTarget()
	target.Library = &testkit.Library{Cached: testkit.SampleLibrary()}
	o = newService().GlobalSearch(t.Context(), target, "zardoz", nil)
	assert.Equal(t, "match.not_found", o.Say.ID)
	assert.Equal(t, "zardoz", o.Say.Params["query"])
	assert.NotNil(t, o.Match)
	assert.Empty(t, backend.Calls())
}

func TestLibrarySearchLeavesOutDisabledContent(t *testing.T) {
	target, backend, _ := newTarget()
	target.Disabled = []media.Content{media.ContentMusic}

	o := newService().GlobalSearch(t.Context(), target, "queen", nil)
	assert.Equal(t, "match.not_found", o.Say.ID, "artists aren't searched")
	assert.Equal(t, "content.disabled", newService().GlobalSearch(t.Context(), target, "queen", []media.Kind{media.KindArtist, media.KindAlbum}).Say.ID)
	assert.Empty(t, backend.Calls())
}

func TestLibrarySearchMatcherFailure(t *testing.T) {
	target, _, _ := newTarget()
	target.Matcher = &brokenMatcher{}

	assertFailed(t, newService().GlobalSearch(t.Context(), target, "dune", nil))
}

func TestGlobalSearchMode(t *testing.T) {
	target, backend, _ := newTarget()
	withAddons(backend)
	target.SearchMode = media.SearchModeGlobal

	o := newService().GlobalSearch(t.Context(), target, "dune", []media.Kind{media.KindMovie})
	assert.Equal(t, "addon.searching", o.Say.ID)
	assert.Empty(t, o.Note)
	assert.Equal(t, []string{"Addons", "ExecuteAddon script.globalsearch searchstring=dune"}, backend.Calls(), "Global Search searches everything")

	target, backend, _ = newTarget()
	target.SearchMode = media.SearchModeGlobal

	o = newService().GlobalSearch(t.Context(), target, "hot fuzz", nil)
	assert.Equal(t, "addon.searching", o.Say.ID)
	assert.Equal(t, "global search: script.globalsearch isn't installed; showed the library", o.Note)
	assert.Equal(t, []string{"Addons", "ShowView movies title hot fuzz"}, backend.Calls())

	target, backend, _ = newTarget()
	withAddons(backend)
	target.SearchMode = media.SearchModeGlobal
	target, _ = failing(target, backend, errBoom, "ExecuteAddon")

	o = newService().GlobalSearch(t.Context(), target, "hot fuzz", nil)
	assert.Equal(t, "addon.searching", o.Say.ID)
	require.NoError(t, o.Err)
	assert.Equal(t, "global search: running script.globalsearch failed: boom; showed the library", o.Note)
	assert.Contains(t, backend.Calls(), "ShowView movies title hot fuzz")
}

func TestSkinSearch(t *testing.T) {
	target, backend, _ := newTarget()
	backend.SetSkin(media.Skin{ID: "skin.arctic.fuse.3", Profile: "skin.arctic.fuse.3", Search: media.SkinSearchSupported}, nil)
	target.SearchMode = media.SearchModeSkin
	target.SkinProfile = "skin.arctic.fuse.2"

	o := newService().GlobalSearch(t.Context(), target, "dune", []media.Kind{media.KindMovie})
	assert.Equal(t, "addon.searching", o.Say.ID)
	assert.Equal(t, "dune", o.Say.Params["query"])
	require.NoError(t, o.Err)
	assert.Equal(t, "skin search: opened skin.arctic.fuse.3's search", o.Note)
	assert.Equal(t, []string{"SkinSearch dune profile=skin.arctic.fuse.2"}, backend.Calls(), "the skin's search searches everything")

	backend.SetSkin(media.Skin{ID: "skin.af2.fork", Profile: "skin.arctic.fuse.2", Search: media.SkinSearchSupported}, nil)
	o = newService().GlobalSearch(t.Context(), target, "dune", nil)
	assert.Equal(t, "skin search: opened skin.af2.fork's search as skin.arctic.fuse.2", o.Note)
}

func TestSkinSearchFallsBackToTheLibrary(t *testing.T) {
	tests := []struct {
		name string
		skin media.Skin
		err  error
		fail bool
		note string
	}{
		{
			name: "no profile", skin: media.Skin{ID: "skin.copacetic", Search: media.SkinSearchNoProfile},
			note: "skin search: skin.copacetic has no profile; showed the library",
		},
		{
			name: "skins Global Search, which isn't installed", skin: media.Skin{ID: "skin.estuary", Profile: "skin.estuary", Search: media.SkinSearchNative},
			note: "skin search: skin.estuary skins Global Search, but script.globalsearch isn't installed; showed the library",
		},
		{
			name: "missing add-ons", skin: media.Skin{ID: "skin.arctic.fuse.3", Profile: "skin.arctic.fuse.3", Search: media.SkinSearchMissingAddons, Missing: []string{"script.skinvariables"}},
			note: "skin search: skin.arctic.fuse.3 needs script.skinvariables; showed the library",
		},
		{
			name: "step failed", skin: media.Skin{ID: "skin.arctic.fuse.3", Profile: "skin.arctic.fuse.3", Search: media.SkinSearchSupported}, err: fmt.Errorf("kodi: skin.arctic.fuse.3 search: %w", errBoom),
			note: "skin search: skin.arctic.fuse.3 failed: kodi: skin.arctic.fuse.3 search: boom; showed the library",
		},
		{
			name: "skin unreadable", fail: true,
			note: "skin search: couldn't read the skin: boom; showed the library",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target, backend, _ := newTarget()
			if tt.skin.Search != media.SkinSearchNative {
				withAddons(backend)
			}

			backend.SetSkin(tt.skin, tt.err)
			target.SearchMode = media.SearchModeSkin

			if tt.fail {
				target, _ = failing(target, backend, errBoom, "SkinSearch")
			}

			o := newService().GlobalSearch(t.Context(), target, "hot fuzz", nil)
			assert.Equal(t, "addon.searching", o.Say.ID)
			require.NoError(t, o.Err)
			assert.Equal(t, tt.note, o.Note)

			calls := backend.Calls()
			assert.Equal(t, "ShowView movies title hot fuzz", calls[len(calls)-1], "the library is shown, not Global Search")
			assert.NotContains(t, calls, "ExecuteAddon script.globalsearch searchstring=hot fuzz")
		})
	}

	target, backend, _ := newTarget()
	target.SearchMode = media.SearchModeSkin
	assert.Equal(t, "remote.no_text", newService().GlobalSearch(t.Context(), target, "  ", nil).Say.ID)
	assert.Empty(t, backend.Calls())
}

func TestAnswerByKind(t *testing.T) {
	film := Choice{Ref: media.Ref{Kind: media.KindMovie, ID: 1}, Title: "Dune"}
	show := Choice{Ref: media.Ref{Kind: media.KindTVShow, ID: 10}, Title: "Dune: Prophecy"}
	other := Choice{Ref: media.Ref{Kind: media.KindMovie, ID: 2}, Title: "Dune"}

	c, ok := at(AnswerShow, []Choice{film, show})
	assert.True(t, ok)
	assert.Equal(t, show, c)

	c, ok = at(AnswerFilm, []Choice{show, film})
	assert.True(t, ok)
	assert.Equal(t, film, c)

	_, ok = at(AnswerShow, []Choice{film, other})
	assert.False(t, ok, "no show was offered")

	_, ok = at(AnswerFilm, []Choice{film, other})
	assert.False(t, ok, "two films were offered")
}

func TestSkinSearchOnASkinThatDrawsGlobalSearch(t *testing.T) {
	target, backend, _ := newTarget()
	withAddons(backend)
	backend.SetSkin(media.Skin{ID: "skin.estuary", Profile: "skin.estuary", Search: media.SkinSearchNative}, nil)
	target.SearchMode = media.SearchModeSkin

	o := newService().GlobalSearch(t.Context(), target, "dune", nil)
	assert.Equal(t, "addon.searching", o.Say.ID)
	assert.Equal(t, "skin search: skin.estuary skins Global Search; used Global Search", o.Note)
	assert.Equal(t, []string{"SkinSearch dune profile=", "Addons", "ExecuteAddon script.globalsearch searchstring=dune"}, backend.Calls(), "its own search is Global Search")
}
