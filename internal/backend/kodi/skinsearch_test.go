package kodi

import (
	"context"
	"encoding/json"
	"net/url"
	"slices"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/backend/kodi/fakekodi"
	"github.com/nightah/encore/internal/core/media"
)

// skinBackend is a backend on a fake running skin with the add-ons installed and enabled.
func skinBackend(t *testing.T, skin string, addons ...string) (*Backend, *fakekodi.Server) {
	t.Helper()

	b, s := newBackend(t, fakekodi.Options{}, BackendOptions{})
	s.SetSkin(skin)
	s.Update(func(l *fakekodi.Library) {
		l.Addons = []fakekodi.Addon{{AddonID: skin, Type: "xbmc.gui.skin", Name: "Skin " + skin, Enabled: true}}
		for _, id := range addons {
			l.Addons = append(l.Addons, fakekodi.Addon{AddonID: id, Type: "xbmc.python.script", Name: id, Enabled: true})
		}
	})

	return b, s
}

// uiCalls lists the calls that change what Kodi shows, in order, as "method params" with the params compacted. A builtin run through script.skinvariables' plugin shows as "builtin <command>".
func uiCalls(t *testing.T, s *fakekodi.Server) []string {
	t.Helper()

	var out []string

	for _, c := range s.Calls() {
		if c.Method == "Files.GetDirectory" {
			var p struct {
				Directory string `json:"directory"`
			}
			require.NoError(t, json.Unmarshal(c.Params, &p))

			rest, ok := strings.CutPrefix(p.Directory, "plugin://script.skinvariables/?info=run_executebuiltin&command=")
			if !ok {
				continue
			}

			command, rules, ok := strings.Cut(rest, "&&")
			require.True(t, ok, p.Directory)
			assert.Equal(t, "special://skin/shortcuts/builtins/skinvariables-closeosd.json", rules)

			builtin, err := url.QueryUnescape(command)
			require.NoError(t, err)

			out = append(out, "builtin "+builtin)

			continue
		}

		if c.Method != "Addons.ExecuteAddon" && c.Method != "Input.SendText" && c.Method != "GUI.ActivateWindow" {
			continue
		}

		var v any
		require.NoError(t, json.Unmarshal(c.Params, &v))
		p, err := json.Marshal(v)
		require.NoError(t, err)

		out = append(out, c.Method+" "+string(p))
	}

	return out
}

// TestSkinVariablesRunsThroughItsPlugin guards against running script.skinvariables with Addons.ExecuteAddon: the add-on is also a plugin, which Kodi's RunAddon prefers, so Kodi opens the plugin's listing instead of running the script.
func TestSkinVariablesRunsThroughItsPlugin(t *testing.T) {
	for _, p := range skinProfiles {
		if !slices.Contains(p.Requires, addonSkinVariables) {
			continue
		}

		t.Run(p.ID, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				b, s := skinBackend(t, p.ID, addonSkinVariables)
				s.ShowWindow(p.Window, 0)

				_, err := b.SkinSearch(t.Context(), "dune", "")
				require.NoError(t, err)

				for _, c := range s.CallsTo("Addons.ExecuteAddon") {
					assert.NotContains(t, string(c.Params), addonSkinVariables)
				}

				assert.NotEmpty(t, s.CallsTo("Files.GetDirectory"), "builtins run through the plugin")
			})
		})
	}
}

func TestSkinSearchProfiles(t *testing.T) {
	testCases := []struct {
		name     string
		skin     string
		addons   []string
		windows  map[string]int
		expected []string
	}{
		{
			name: "ShouldSearchArcticFuse3", skin: "skin.arctic.fuse.3", addons: []string{addonSkinVariables}, windows: map[string]int{"1105": 2},
			expected: []string{
				`builtin ReplaceWindow(1105)`,
				`builtin SetFocus(3000)`,
				`Input.SendText {"done":true,"text":"dune, part \"two\""}`,
			},
		},
		{
			name: "ShouldSearchArcticFuse2", skin: "skin.arctic.fuse.2", addons: []string{addonSkinVariables}, windows: map[string]int{"1185": 1},
			expected: []string{
				`builtin ActivateWindow(1185)`,
				`builtin SetFocus(9099)`,
				`Input.SendText {"done":true,"text":"dune, part \"two\""}`,
			},
		},
		{
			name: "ShouldSearchArcticZephyr2ResurrectionMod", skin: "skin.arctic.zephyr.2.resurrection.mod", addons: []string{addonSkinVariables}, windows: map[string]int{"1138": 0},
			expected: []string{
				`builtin Skin.SetString(SearchTerm,"dune, part \"two\"")`,
				`builtin ActivateWindow(1138)`,
			},
		},
		{
			name: "ShouldSearchNimbus", skin: "skin.nimbus", addons: []string{addonNimbusHelper}, windows: map[string]int{"home": 0, "1121": 3, "virtualkeyboard": 1},
			expected: []string{
				`GUI.ActivateWindow {"window":"home"}`,
				`Addons.ExecuteAddon {"addonid":"script.nimbus.helper","params":["mode=open_search_window"]}`,
				`Addons.ExecuteAddon {"addonid":"script.nimbus.helper","params":["mode=search_input"]}`,
				`Input.SendText {"done":true,"text":"dune, part \"two\""}`,
			},
		},
		{
			// FENtastic's helper may reload the skin before its window opens, which takes longer than other windows get.
			name: "ShouldSearchFENtastic", skin: "skin.fentastic", addons: []string{addonFENtasticHelper}, windows: map[string]int{"home": 0, "1121": int(5 * time.Second / windowPollInterval), "virtualkeyboard": 0},
			expected: []string{
				`GUI.ActivateWindow {"window":"home"}`,
				`Addons.ExecuteAddon {"addonid":"script.fentastic.helper","params":["mode=open_search_window"]}`,
				`Addons.ExecuteAddon {"addonid":"script.fentastic.helper","params":["mode=search_input"]}`,
				`Input.SendText {"done":true,"text":"dune, part \"two\""}`,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				b, s := skinBackend(t, tc.skin, tc.addons...)
				for w, n := range tc.windows {
					s.ShowWindow(w, n)
				}

				skin, err := b.SkinSearch(t.Context(), "dune,  part \"two\"", "")
				require.NoError(t, err)
				assert.Equal(t, media.SkinSearchSupported, skin.Search)
				assert.Equal(t, tc.skin, skin.ID)
				assert.Equal(t, tc.skin, skin.Profile)
				assert.Equal(t, tc.expected, uiCalls(t, s))
			})
		})
	}
}

func TestSkinSearchWaitsForTheWindowBeforeTyping(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b, s := skinBackend(t, "skin.arctic.fuse.3", addonSkinVariables)
		s.ShowWindow("1105", 4)

		start := time.Now()
		_, err := b.SkinSearch(t.Context(), "dune", "")
		require.NoError(t, err)

		polls := 0

		for _, c := range s.Calls() {
			if c.Method == "XBMC.GetInfoBooleans" {
				polls++

				assert.JSONEq(t, `{"booleans":["Window.IsVisible(1105)"]}`, string(c.Params))
			}
		}

		assert.Equal(t, 5, polls, "polls until the window shows")
		assert.Equal(t, 4*windowPollInterval, time.Since(start), "waits between polls")
	})
}

func TestSkinSearchLeavesOtherSkinsAlone(t *testing.T) {
	testCases := []struct {
		name     string
		skin     string
		profile  string
		addons   []string
		expected media.Skin
	}{
		{name: "ShouldReportUnknownSkin", skin: "skin.copacetic", expected: media.Skin{ID: "skin.copacetic", Search: media.SkinSearchNoProfile}},
		{name: "ShouldLeaveSkinThatDrawsGlobalSearch", skin: "skin.aeon.nox.silvo", expected: media.Skin{ID: "skin.aeon.nox.silvo", Name: "Aeon Nox: SiLVO", Profile: "skin.aeon.nox.silvo", Search: media.SkinSearchNative}},
		{name: "ShouldReportMissingRequiredAddon", skin: "skin.arctic.fuse.3", addons: []string{"script.globalsearch"}, expected: media.Skin{ID: "skin.arctic.fuse.3", Name: "Arctic Fuse 3", Profile: "skin.arctic.fuse.3", Search: media.SkinSearchMissingAddons, Missing: []string{addonSkinVariables}}},
		{name: "ShouldNotLetGlobalSearchProfileStandIn", skin: "skin.ciro", profile: "skin.estuary", expected: media.Skin{ID: "skin.ciro", Search: media.SkinSearchNoProfile}},
		{name: "ShouldIgnoreUnknownProfile", skin: "skin.ciro", profile: "skin.nope", expected: media.Skin{ID: "skin.ciro", Search: media.SkinSearchNoProfile}},
		{name: "ShouldPreferSkinsOwnProfileOverChosenOne", skin: "skin.aeon.tajo", profile: "skin.arctic.fuse.2", addons: []string{addonSkinVariables}, expected: media.Skin{ID: "skin.aeon.tajo", Name: "Aeon Tajo", Profile: "skin.aeon.tajo", Search: media.SkinSearchNative}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				b, s := skinBackend(t, tc.skin, tc.addons...)

				skin, err := b.SkinSearch(t.Context(), "dune", tc.profile)
				require.NoError(t, err)
				assert.Equal(t, tc.expected, skin)
				assert.Empty(t, uiCalls(t, s), "nothing is opened")
			})
		})
	}
}

func TestSkinSearchWithAChosenProfile(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b, s := skinBackend(t, "skin.arctic.fuse.2.mod", addonSkinVariables)
		s.ShowWindow("1185", 0)

		skin, err := b.SkinSearch(t.Context(), "dune", "skin.arctic.fuse.2")
		require.NoError(t, err)
		assert.Equal(t, media.Skin{ID: "skin.arctic.fuse.2.mod", Profile: "skin.arctic.fuse.2", Search: media.SkinSearchSupported}, skin)
		assert.Equal(t, []string{
			`builtin ActivateWindow(1185)`,
			`builtin SetFocus(9099)`,
			`Input.SendText {"done":true,"text":"dune"}`,
		}, uiCalls(t, s))
	})
}

func TestSkinSearchFailures(t *testing.T) {
	t.Run("ShouldGiveUpWhenWindowNeverOpens", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			b, s := skinBackend(t, "skin.arctic.fuse.3", addonSkinVariables)
			s.ShowWindow("1105", -1)

			start := time.Now()
			skin, err := b.SkinSearch(t.Context(), "dune", "")
			require.ErrorIs(t, err, errWindowNotVisible)
			assert.Contains(t, err.Error(), "skin.arctic.fuse.3")
			assert.Equal(t, media.SkinSearchSupported, skin.Search, "the skin is still reported")
			assert.LessOrEqual(t, time.Since(start), windowWait, "gives up in time")
			assert.Len(t, uiCalls(t, s), 1, "nothing is typed")
		})
	})

	t.Run("ShouldGiveUpWhenSearchWindowNeverOpens", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			b, s := skinBackend(t, "skin.nimbus", addonNimbusHelper)
			s.ShowWindow("home", 0)
			s.ShowWindow("1121", -1)

			_, err := b.SkinSearch(t.Context(), "dune", "")
			require.ErrorIs(t, err, errWindowNotVisible)
			assert.NotContains(t, strings.Join(uiCalls(t, s), "\n"), "search_input", "the keyboard isn't opened")
		})
	})

	t.Run("ShouldGiveUpWhenKeyboardNeverOpens", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			b, s := skinBackend(t, "skin.nimbus", addonNimbusHelper)
			s.ShowWindow("home", 0)
			s.ShowWindow("1121", 0)

			_, err := b.SkinSearch(t.Context(), "dune", "")
			require.ErrorIs(t, err, errWindowNotVisible)
			assert.NotContains(t, strings.Join(uiCalls(t, s), "\n"), "Input.SendText")
		})
	})

	testCases := []struct {
		name   string
		method string
	}{
		{name: "ShouldFailWhenExecuteAddonFails", method: "Addons.ExecuteAddon"},
		{name: "ShouldFailWhenGetInfoBooleansFails", method: "XBMC.GetInfoBooleans"},
		{name: "ShouldFailWhenActivateWindowFails", method: "GUI.ActivateWindow"},
		{name: "ShouldFailWhenSendTextFails", method: "Input.SendText"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				b, s := skinBackend(t, "skin.nimbus", addonNimbusHelper)
				for _, w := range []string{"home", "1121", "virtualkeyboard"} {
					s.ShowWindow(w, 0)
				}

				s.Handle(tc.method, func(json.RawMessage) (any, *fakekodi.Error) {
					return nil, &fakekodi.Error{Code: CodeInternalError, Message: "Internal error."}
				})

				_, err := b.SkinSearch(t.Context(), "dune", "")
				require.Error(t, err)
			})
		})
	}

	t.Run("ShouldFailWhenEachHelperStepFails", func(t *testing.T) {
		steps := []struct {
			name string
			fail string
		}{
			{name: "ShouldFailWhenOpenSearchWindowFails", fail: "mode=open_search_window"},
			{name: "ShouldFailWhenSearchInputFails", fail: "mode=search_input"},
		}

		for _, step := range steps {
			t.Run(step.name, func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					b, s := skinBackend(t, "skin.nimbus", addonNimbusHelper)
					for _, w := range []string{"home", "1121", "virtualkeyboard"} {
						s.ShowWindow(w, 0)
					}

					s.Handle("Addons.ExecuteAddon", func(p json.RawMessage) (any, *fakekodi.Error) {
						if strings.Contains(string(p), step.fail) {
							return nil, &fakekodi.Error{Code: CodeInvalidParams, Message: "Invalid params."}
						}

						return "OK", nil
					})

					_, err := b.SkinSearch(t.Context(), "dune", "")
					require.Error(t, err)
				})
			})
		}
	})

	t.Run("ShouldGiveUpWhenHomeNeverShows", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			b, s := skinBackend(t, "skin.fentastic", addonFENtasticHelper)

			_, err := b.SkinSearch(t.Context(), "dune", "")
			require.ErrorIs(t, err, errWindowNotVisible)
			assert.Len(t, uiCalls(t, s), 1, "the helper isn't run")
		})
	})

	t.Run("ShouldFailWhenWindowOpeningStepFails", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			b, s := skinBackend(t, "skin.arctic.fuse.3", addonSkinVariables)
			s.Handle("Files.GetDirectory", func(json.RawMessage) (any, *fakekodi.Error) {
				return nil, &fakekodi.Error{Code: CodeInvalidParams, Message: "Invalid params."}
			})

			_, err := b.SkinSearch(t.Context(), "dune", "")
			require.Error(t, err)
			assert.Empty(t, s.CallsTo("XBMC.GetInfoBooleans"), "doesn't wait for a window that wasn't asked for")
		})
	})

	t.Run("ShouldStopWhenCancelledWhileWaiting", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			b, s := skinBackend(t, "skin.arctic.fuse.3", addonSkinVariables)
			s.ShowWindow("1105", -1)

			ctx, cancel := context.WithTimeout(t.Context(), windowPollInterval*3/2)
			defer cancel()

			_, err := b.SkinSearch(ctx, "dune", "")
			require.ErrorIs(t, err, context.DeadlineExceeded)
		})
	})
}

func TestSkinReadFailures(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b, s := skinBackend(t, "skin.arctic.fuse.3", addonSkinVariables)

		s.Handle("Addons.GetAddons", func(json.RawMessage) (any, *fakekodi.Error) {
			return nil, &fakekodi.Error{Code: CodeInternalError, Message: "Internal error."}
		})

		skin, err := b.SkinSearch(t.Context(), "dune", "")
		require.Error(t, err)
		assert.Equal(t, "skin.arctic.fuse.3", skin.ID, "the skin read is reported")
		_, err = b.Skin(t.Context(), "")
		require.Error(t, err)

		s.Handle("Settings.GetSettingValue", func(json.RawMessage) (any, *fakekodi.Error) {
			return map[string]any{"value": 3}, nil
		})

		_, err = b.SkinSearch(t.Context(), "dune", "")
		require.ErrorContains(t, err, "lookandfeel.skin")

		s.Handle("Settings.GetSettingValue", func(json.RawMessage) (any, *fakekodi.Error) {
			return nil, &fakekodi.Error{Code: CodeInvalidParams, Message: "Invalid params."}
		})

		_, err = b.Skin(t.Context(), "")
		require.Error(t, err)
		assert.Empty(t, uiCalls(t, s))
	})
}

func TestSkin(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b, s := skinBackend(t, "skin.arctic.fuse.3", addonSkinVariables)

		skin, err := b.Skin(t.Context(), "")
		require.NoError(t, err)
		assert.Equal(t, media.Skin{ID: "skin.arctic.fuse.3", Name: "Arctic Fuse 3", Profile: "skin.arctic.fuse.3", Search: media.SkinSearchSupported}, skin)

		s.SetSkin("skin.copacetic")
		s.Update(func(l *fakekodi.Library) {
			l.Addons = append(l.Addons, fakekodi.Addon{AddonID: "skin.copacetic", Type: "xbmc.gui.skin", Name: "Copacetic", Enabled: true})
		})

		skin, err = b.Skin(t.Context(), "")
		require.NoError(t, err)
		assert.Equal(t, media.Skin{ID: "skin.copacetic", Name: "Copacetic", Search: media.SkinSearchNoProfile}, skin, "a skin change shows at once, named as Kodi names it")

		s.SetSkin("skin.gone")

		skin, err = b.Skin(t.Context(), "skin.arctic.fuse.2")
		require.NoError(t, err)
		assert.Equal(t, media.Skin{ID: "skin.gone", Name: "skin.gone", Profile: "skin.arctic.fuse.2", Search: media.SkinSearchSupported}, skin, "a skin Kodi can't describe is named by its ID")
		assert.Empty(t, uiCalls(t, s), "reading the skin opens nothing")
	})
}

func TestSkinProfiles(t *testing.T) {
	t.Parallel()

	assert.True(t, slices.IsSortedFunc(skinProfiles, func(a, b skinProfile) int { return strings.Compare(a.ID, b.ID) }), "sorted for lookup")

	for _, p := range skinProfiles {
		got, ok := lookupSkinProfile(p.ID)
		assert.True(t, ok, p.ID)
		assert.Equal(t, p.ID, got.ID)
		assert.NotEmpty(t, p.Name, p.ID)

		if p.Kind == skinSearchNative {
			assert.Nil(t, p.run, "%s runs Global Search", p.ID)
			assert.Empty(t, p.Requires, p.ID)
		} else {
			assert.NotNil(t, p.run, p.ID)
			assert.NotEmpty(t, p.Requires, p.ID)
			assert.NotEmpty(t, p.Window, p.ID)
		}
	}

	profiles := SkinProfiles()
	require.Len(t, profiles, len(skinProfiles))
	assert.Equal(t, SkinProfile{ID: "skin.arctic.fuse.3", Name: "Arctic Fuse 3", Requires: []string{addonSkinVariables}, Window: "1105", How: "edit_control"}, profiles[slices.IndexFunc(profiles, func(p SkinProfile) bool { return p.ID == "skin.arctic.fuse.3" })])
	assert.Equal(t, SkinProfile{ID: "skin.estuary", Name: "Estuary", Native: true, Requires: []string{}, How: "native_global_search"}, profiles[slices.IndexFunc(profiles, func(p SkinProfile) bool { return p.ID == "skin.estuary" })])

	profiles[0].Requires = append(profiles[0].Requires, "changed")
	assert.NotContains(t, skinProfiles[0].Requires, "changed", "callers get copies")

	assert.Equal(t, []string{"skin.arctic.fuse.2", "skin.arctic.fuse.3", "skin.arctic.zephyr.2.resurrection.mod", "skin.fentastic", "skin.nimbus"}, SkinProfileIDs())
	assert.True(t, IsSkinProfile("skin.nimbus"))
	assert.False(t, IsSkinProfile("skin.estuary"), "runs Global Search")
	assert.False(t, IsSkinProfile("skin.unknown"))
	assert.False(t, IsSkinProfile(""))
}

func TestBuiltinParam(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		have     string
		expected string
	}{
		{name: "ShouldQuotePlainWord", have: "dune", expected: `"dune"`},
		{name: "ShouldKeepComma", have: "dune, part two", expected: `"dune, part two"`},
		{name: "ShouldEscapeDoubleQuotes", have: `say "hello"`, expected: `"say \"hello\""`},
		{name: "ShouldEscapeBackslash", have: `back\slash`, expected: `"back\\slash"`},
		{name: "ShouldEscapeTrailingBackslash", have: `ends with \`, expected: `"ends with \\"`},
		{name: "ShouldEscapeBackslashBeforeQuote", have: `\"`, expected: `"\\\""`},
		{name: "ShouldKeepApostrophe", have: "it's", expected: `"it's"`},
		{name: "ShouldKeepSingleQuotes", have: "'quoted'", expected: `"'quoted'"`},
		{name: "ShouldKeepParentheses", have: "(500) days of summer", expected: `"(500) days of summer"`},
		{name: "ShouldKeepUnbalancedClosingParenthesis", have: "closing) paren", expected: `"closing) paren"`},
		{name: "ShouldKeepAmpersand", have: "tom & jerry", expected: `"tom & jerry"`},
		{name: "ShouldKeepEquals", have: "x = y", expected: `"x = y"`},
		{name: "ShouldBreakDoublePipe", have: "a||b", expected: `"a b"`},
		{name: "ShouldBreakDoublePipeInTriplePipe", have: "a|||b", expected: `"a |b"`},
		{name: "ShouldKeepSpacedPipe", have: "a | b", expected: `"a | b"`},
		{name: "ShouldKeepInfoLabel", have: "$INFO[System.Time]", expected: `"$INFO[System.Time]"`},
		{name: "ShouldCollapseWhitespace", have: "  lots\tof \n space  ", expected: `"lots of space"`},
		{name: "ShouldReplaceControlCharacters", have: "bell\x07and\x00nul", expected: `"bell and nul"`},
		{name: "ShouldKeepJapanese", have: "千と千尋の神隠し", expected: `"千と千尋の神隠し"`},
		{name: "ShouldKeepArabic", have: "الرسالة", expected: `"الرسالة"`},
		{name: "ShouldKeepDevanagari", have: "दिलवाले दुल्हनिया", expected: `"दिलवाले दुल्हनिया"`},
		{name: "ShouldKeepEmoji", have: "🎬 movie", expected: `"🎬 movie"`},
		{name: "ShouldQuoteEmpty", expected: `""`},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actual := builtinParam(tc.have)
			assert.Equal(t, tc.expected, actual)
			assert.Equal(t, []string{"x", cleanQuery(tc.have), "y"}, splitParams("x,"+actual+",y"), "Kodi reads it back as one parameter")

			// Addons.ExecuteAddon quotes each argument of an array as Kodi's StringUtils::Paramify does, then RunAddon splits them again.
			args := []string{"run_executebuiltin=SetProperty(CustomSearchTerm," + actual + ",Home)||ActivateWindow(1114)", "text=" + cleanQuery(tc.have)}
			assert.Equal(t, args, splitParams(paramify(args[0])+","+paramify(args[1])), "it reaches the add-on as sent")
		})
	}
}

func paramify(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

// splitParams splits a builtin's parameters as Kodi's CUtil::SplitParams does, for checking builtinParam against.
func splitParams(s string) []string {
	var (
		out                    []string
		param                  []byte
		inQuotes, lastEscaped  bool
		inFunction, whiteSpace int
	)

	trim := func(p []byte) string {
		if len(p) > 1 && p[0] == '"' && p[len(p)-1] == '"' {
			return string(p[1 : len(p)-1])
		}

		return string(p)
	}

	for pos := 0; pos < len(s); pos++ {
		ch := s[pos]
		escaped := pos > 0 && s[pos-1] == '\\' && !lastEscaped
		lastEscaped = escaped

		if inQuotes {
			if ch == '"' && !escaped {
				inQuotes = false
			}
		} else {
			if ch == '"' && !escaped {
				inQuotes = true
			}

			if inFunction > 0 && ch == ')' {
				inFunction--
			}

			if ch == '(' {
				inFunction++
			}

			if inFunction == 0 && ch == ',' {
				if whiteSpace > 0 {
					param = param[:whiteSpace]
				}

				out = append(out, trim(param))
				param, whiteSpace = nil, 0

				continue
			}
		}

		if (ch == '"' || ch == '\\') && escaped {
			param[len(param)-1] = ch

			continue
		}

		if ch == ' ' && !inQuotes {
			if len(param) == 0 {
				continue
			}

			if whiteSpace == 0 {
				whiteSpace = len(param)
			}
		} else {
			whiteSpace = 0
		}

		param = append(param, ch)
	}

	if whiteSpace > 0 {
		param = param[:whiteSpace]
	}

	return append(out, trim(param))
}

func TestSkinCommandFailures(t *testing.T) {
	testCases := []struct {
		name   string
		skin   string
		window string
		fail   string
	}{
		{name: "ShouldFailWhenArcticFuse3SetFocusFails", skin: "skin.arctic.fuse.3", window: "1105", fail: "SetFocus"},
		{name: "ShouldFailWhenArcticZephyrSkinSetStringFails", skin: "skin.arctic.zephyr.2.resurrection.mod", window: "1138", fail: "Skin.SetString"},
		{name: "ShouldFailWhenArcticZephyrActivateWindowFails", skin: "skin.arctic.zephyr.2.resurrection.mod", window: "1138", fail: "ActivateWindow"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				b, s := skinBackend(t, tc.skin, addonSkinVariables)
				s.ShowWindow(tc.window, 0)
				s.Handle("Files.GetDirectory", func(p json.RawMessage) (any, *fakekodi.Error) {
					if strings.Contains(string(p), tc.fail) {
						return nil, &fakekodi.Error{Code: CodeInvalidParams, Message: "Invalid params."}
					}

					return map[string]any{"files": []any{}, "limits": map[string]int{"start": 0, "end": 0, "total": 0}}, nil
				})

				_, err := b.SkinSearch(t.Context(), "dune", "")
				require.Error(t, err)
				assert.NotContains(t, strings.Join(uiCalls(t, s), "\n"), "Input.SendText", "nothing is typed")
			})
		})
	}
}
