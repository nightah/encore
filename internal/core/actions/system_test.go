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
	testCases := []struct {
		name     string
		have     string
		expected string
	}{
		{name: "ShouldShowAFilm", have: "hot fuzz", expected: "ShowView movies title hot fuzz"},
		{name: "ShouldShowAShow", have: "severance", expected: "ShowView tvshows title severance"},
		{name: "ShouldShowAnArtist", have: "queen", expected: "ShowView artists title queen"},
		{name: "ShouldShowAnAlbum", have: "innuendo", expected: "ShowView albums title innuendo"},
		{name: "ShouldShowAMusicVideo", have: "bohemian rhapsody", expected: "ShowView musicvideos title bohemian rhapsody"},
		{name: "ShouldShowAFilmForPartOfItsTitle", have: "fuzz", expected: "ShowView movies title fuzz"},
		{name: "ShouldShowAFilmInACollection", have: "dune", expected: "ShowView movies title dune"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			target, backend, _ := newTarget()

			o := newService().GlobalSearch(t.Context(), target, tc.have, nil)
			assert.Equal(t, "addon.searching", o.Say.ID)
			assert.Equal(t, tc.have, o.Say.Params["query"])
			assert.Empty(t, o.Note, "the library is the default, not a fallback")
			require.NotNil(t, o.Match, "the match is logged")
			assert.Equal(t, []string{tc.expected}, backend.Calls())
		})
	}
}

func TestLibrarySearchTieBreaks(t *testing.T) {
	soundtrack := media.Item{Kind: media.KindAlbum, ID: 43, Title: "Hot Fuzz", Artist: "Various"}
	theColony := media.Item{Kind: media.KindMovie, ID: 6, Title: "Colony"}
	colony := media.Item{Kind: media.KindTVShow, ID: 21, Title: "Colony"}

	testCases := []struct {
		name     string
		items    []media.Item
		matcher  matcherFunc
		have     string
		kinds    []media.Kind
		expected []string
	}{
		{name: "ShouldPreferAFilmToItsSoundtrack", items: append(testkit.SampleLibrary(), soundtrack), have: "hot fuzz", expected: []string{"ShowView movies title hot fuzz"}},
		{name: "ShouldPreferTheSoundtrackWhenMusicWasAskedFor", items: []media.Item{soundtrack, testkit.SampleLibrary()[3]}, have: "hot fuzz", kinds: []media.Kind{media.KindAlbum}, expected: []string{"ShowView albums title hot fuzz"}},
		// "The Colony" scores as "Colony" does in the real matcher; the exact title wins the tie.
		{
			name:  "ShouldPreferTheExactTitleInATie",
			items: []media.Item{colony, theColony},
			matcher: func(match.Query) match.Result {
				return match.Result{Normalized: "colony", Decision: match.DecisionAsk, Candidates: []match.Candidate{
					{Doc: match.Doc{ID: "movie/6", Kind: media.KindMovie, Ref: theColony.Ref(), Title: "The Colony"}, Score: 1},
					{Doc: match.Doc{ID: "tvshow/21", Kind: media.KindTVShow, Ref: colony.Ref(), Title: "Colony"}, Score: 1},
				}}
			},
			have:     "colony",
			expected: []string{"ShowView tvshows title colony"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			target, backend, _ := newTarget(tc.items...)

			if tc.matcher != nil {
				target.Matcher = tc.matcher
			}

			newService().GlobalSearch(t.Context(), target, tc.have, tc.kinds)
			assert.Equal(t, tc.expected, backend.Calls())
		})
	}
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

	pending, search := o.Pending, *o.Followup.Search

	testCases := []struct {
		name     string
		have     Answer
		expected []string
	}{
		{name: "ShouldShowTheShowWhenTheShowWasChosen", have: Answer{Which: AnswerShow}, expected: []string{"ShowView tvshows title severance"}},
		{name: "ShouldShowTheFilmWhenTheFilmWasChosen", have: Answer{Which: AnswerFilm}, expected: []string{"ShowView movies title severance"}},
		{name: "ShouldShowTheFilmWhenTheFirstWasChosen", have: Answer{Which: AnswerFirst}, expected: []string{"ShowView movies title severance"}},
		{name: "ShouldAskAgainForATitleNamingNeither", have: Answer{Title: "severance"}},
		{name: "ShouldAskAgainForNoAnswer"},
		{name: "ShouldAskAgainForAnUnknownAnswer", have: Answer{Which: AnswerSecond + "x"}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.expected == nil {
				o := svc.ChooseSearch(t.Context(), target, tc.have, pending, search)
				assert.Equal(t, "match.which_one", o.Say.ID, "names neither")
				assert.True(t, o.AskAgain)

				return
			}

			target, backend, _ := newTarget(append(testkit.SampleLibrary(), film)...)

			o := svc.ChooseSearch(t.Context(), target, tc.have, pending, search)
			assert.Equal(t, "addon.searching", o.Say.ID)
			assert.Equal(t, tc.expected, backend.Calls())
		})
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
	testCases := []struct {
		name     string
		have     media.Skin
		skinErr  error
		fail     bool
		expected string
	}{
		{name: "ShouldFallBackForASkinWithoutAProfile", have: media.Skin{ID: "skin.copacetic", Search: media.SkinSearchNoProfile}, expected: "skin search: skin.copacetic has no profile; showed the library"},
		{name: "ShouldFallBackForASkinThatSkinsGlobalSearchWhichIsntInstalled", have: media.Skin{ID: "skin.estuary", Profile: "skin.estuary", Search: media.SkinSearchNative}, expected: "skin search: skin.estuary skins Global Search, but script.globalsearch isn't installed; showed the library"},
		{name: "ShouldFallBackForMissingAddOns", have: media.Skin{ID: "skin.arctic.fuse.3", Profile: "skin.arctic.fuse.3", Search: media.SkinSearchMissingAddons, Missing: []string{"script.skinvariables"}}, expected: "skin search: skin.arctic.fuse.3 needs script.skinvariables; showed the library"},
		{name: "ShouldFallBackWhenAStepFailed", have: media.Skin{ID: "skin.arctic.fuse.3", Profile: "skin.arctic.fuse.3", Search: media.SkinSearchSupported}, skinErr: fmt.Errorf("kodi: skin.arctic.fuse.3 search: %w", errBoom), expected: "skin search: skin.arctic.fuse.3 failed: kodi: skin.arctic.fuse.3 search: boom; showed the library"},
		{name: "ShouldFallBackWhenTheSkinIsUnreadable", fail: true, expected: "skin search: couldn't read the skin: boom; showed the library"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			target, backend, _ := newTarget()
			if tc.have.Search != media.SkinSearchNative {
				withAddons(backend)
			}

			backend.SetSkin(tc.have, tc.skinErr)
			target.SearchMode = media.SearchModeSkin

			if tc.fail {
				target, _ = failing(target, backend, errBoom, "SkinSearch")
			}

			o := newService().GlobalSearch(t.Context(), target, "hot fuzz", nil)
			assert.Equal(t, "addon.searching", o.Say.ID)
			require.NoError(t, o.Err)
			assert.Equal(t, tc.expected, o.Note)

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

	testCases := []struct {
		name     string
		which    string
		have     []Choice
		expected Choice
		ok       bool
	}{
		{name: "ShouldPickTheShow", which: AnswerShow, have: []Choice{film, show}, expected: show, ok: true},
		{name: "ShouldPickTheFilm", which: AnswerFilm, have: []Choice{show, film}, expected: film, ok: true},
		{name: "ShouldNotPickAShowWhenNoneWasOffered", which: AnswerShow, have: []Choice{film, other}},
		{name: "ShouldNotPickAFilmWhenTwoWereOffered", which: AnswerFilm, have: []Choice{film, other}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actual, ok := at(tc.which, tc.have)
			assert.Equal(t, tc.ok, ok)

			if tc.ok {
				assert.Equal(t, tc.expected, actual)
			}
		})
	}
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

// TestLibrarySearchDoesntAskAboutPartialMatches guards against asking "the film or the TV show?" when a film and a show only both contain what was said, which isn't either one's name.
func TestLibrarySearchDoesntAskAboutPartialMatches(t *testing.T) {
	target, backend, _ := newTarget()
	target.Matcher = matcherFunc(func(match.Query) match.Result {
		return match.Result{Normalized: "star", Decision: match.DecisionAsk, Candidates: []match.Candidate{
			{Doc: match.Doc{ID: "tvshow/30", Kind: media.KindTVShow, Ref: media.Ref{Kind: media.KindTVShow, ID: 30}, Title: "Star Trek"}, Score: 0.7},
			{Doc: match.Doc{ID: "movie/30", Kind: media.KindMovie, Ref: media.Ref{Kind: media.KindMovie, ID: 30}, Title: "Star Wars"}, Score: 0.7},
			{Doc: match.Doc{ID: "album/30", Kind: media.KindAlbum, Ref: media.Ref{Kind: media.KindAlbum, ID: 30}, Title: "Star"}, Score: 0.5},
		}}
	})

	o := newService().GlobalSearch(t.Context(), target, "star", nil)
	assert.Equal(t, "addon.searching", o.Say.ID)
	assert.Equal(t, []string{"ShowView movies title star"}, backend.Calls(), "films first, without asking; the album scored lower")
}

func TestSkinSearchWhenKodiFails(t *testing.T) {
	target, backend, _ := newTarget()
	target.SearchMode = media.SearchModeSkin
	backend.Err = errBoom

	assertFailed(t, newService().GlobalSearch(t.Context(), target, "hot fuzz", nil))
}
