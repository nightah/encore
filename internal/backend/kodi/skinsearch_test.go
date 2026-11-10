package kodi

import (
	"context"
	"encoding/json"
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

// uiCalls lists the calls that change what Kodi shows, in order, as "method params" with the params compacted.
func uiCalls(t *testing.T, s *fakekodi.Server) []string {
	t.Helper()

	var out []string

	for _, c := range s.Calls() {
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

func TestSkinSearchProfiles(t *testing.T) {
	tests := []struct {
		skin    string
		addons  []string
		windows map[string]int
		want    []string
	}{
		{
			skin: "skin.arctic.fuse.3", addons: []string{addonSkinVariables}, windows: map[string]int{"1105": 2},
			want: []string{
				`Addons.ExecuteAddon {"addonid":"script.skinvariables","params":["run_executebuiltin=ReplaceWindow(1105)"]}`,
				`Addons.ExecuteAddon {"addonid":"script.skinvariables","params":["set_editcontrol=3000","text=dune, part \"two\""]}`,
			},
		},
		{
			skin: "skin.arctic.fuse.2", addons: []string{addonSkinVariables}, windows: map[string]int{"1185": 1},
			want: []string{
				`Addons.ExecuteAddon {"addonid":"script.skinvariables","params":["run_executebuiltin=SetProperty(CustomSearchTerm,\"dune, part \\\"two\\\"\",Home)||ActivateWindow(1185)"]}`,
			},
		},
		{
			skin: "skin.arctic.horizon.2", addons: []string{addonSkinVariables}, windows: map[string]int{"1114": 0},
			want: []string{
				`Addons.ExecuteAddon {"addonid":"script.skinvariables","params":["run_executebuiltin=SetProperty(CustomSearchTerm,\"dune, part \\\"two\\\"\",Home)||ActivateWindow(1114)"]}`,
			},
		},
		{
			skin: "skin.arctic.zephyr.2.resurrection.mod", addons: []string{addonSkinVariables}, windows: map[string]int{"1138": 0},
			want: []string{
				`Addons.ExecuteAddon {"addonid":"script.skinvariables","params":["run_executebuiltin=Skin.SetString(SearchTerm,\"dune, part \\\"two\\\"\")||ActivateWindow(1138)"]}`,
			},
		},
		{
			skin: "skin.nimbus", addons: []string{addonNimbusHelper}, windows: map[string]int{"home": 0, "1121": 3, "virtualkeyboard": 1},
			want: []string{
				`GUI.ActivateWindow {"window":"home"}`,
				`Addons.ExecuteAddon {"addonid":"script.nimbus.helper","params":["mode=open_search_window"]}`,
				`Addons.ExecuteAddon {"addonid":"script.nimbus.helper","params":["mode=search_input"]}`,
				`Input.SendText {"done":true,"text":"dune, part \"two\""}`,
			},
		},
		{
			// FENtastic's helper may reload the skin before its window opens, which takes longer than other windows get.
			skin: "skin.fentastic", addons: []string{addonFENtasticHelper}, windows: map[string]int{"home": 0, "1121": int(5 * time.Second / windowPollInterval), "virtualkeyboard": 0},
			want: []string{
				`GUI.ActivateWindow {"window":"home"}`,
				`Addons.ExecuteAddon {"addonid":"script.fentastic.helper","params":["mode=open_search_window"]}`,
				`Addons.ExecuteAddon {"addonid":"script.fentastic.helper","params":["mode=search_input"]}`,
				`Input.SendText {"done":true,"text":"dune, part \"two\""}`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.skin, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				b, s := skinBackend(t, tt.skin, tt.addons...)
				for w, n := range tt.windows {
					s.ShowWindow(w, n)
				}

				skin, err := b.SkinSearch(t.Context(), "dune,  part \"two\"", "")
				require.NoError(t, err)
				assert.Equal(t, media.SkinSearchSupported, skin.Search)
				assert.Equal(t, tt.skin, skin.ID)
				assert.Equal(t, tt.skin, skin.Profile)
				assert.Equal(t, tt.want, uiCalls(t, s))
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
	tests := []struct {
		name, skin, profile string
		addons              []string
		want                media.Skin
	}{
		{
			name: "unknown skin", skin: "skin.copacetic",
			want: media.Skin{ID: "skin.copacetic", Search: media.SkinSearchNoProfile},
		},
		{
			name: "skin that draws Global Search", skin: "skin.aeon.nox.silvo",
			want: media.Skin{ID: "skin.aeon.nox.silvo", Name: "Aeon Nox: SiLVO", Profile: "skin.aeon.nox.silvo", Search: media.SkinSearchNative},
		},
		{
			name: "required add-on missing", skin: "skin.arctic.fuse.3", addons: []string{"script.globalsearch"},
			want: media.Skin{ID: "skin.arctic.fuse.3", Name: "Arctic Fuse 3", Profile: "skin.arctic.fuse.3", Search: media.SkinSearchMissingAddons, Missing: []string{addonSkinVariables}},
		},
		{
			name: "a profile that runs Global Search can't stand in", skin: "skin.ciro", profile: "skin.estuary",
			want: media.Skin{ID: "skin.ciro", Search: media.SkinSearchNoProfile},
		},
		{
			name: "unknown profile", skin: "skin.ciro", profile: "skin.nope",
			want: media.Skin{ID: "skin.ciro", Search: media.SkinSearchNoProfile},
		},
		{
			name: "a skin's own profile wins over the one chosen", skin: "skin.aeon.tajo", profile: "skin.arctic.horizon.2", addons: []string{addonSkinVariables},
			want: media.Skin{ID: "skin.aeon.tajo", Name: "Aeon Tajo", Profile: "skin.aeon.tajo", Search: media.SkinSearchNative},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				b, s := skinBackend(t, tt.skin, tt.addons...)

				skin, err := b.SkinSearch(t.Context(), "dune", tt.profile)
				require.NoError(t, err)
				assert.Equal(t, tt.want, skin)
				assert.Empty(t, uiCalls(t, s), "nothing is opened")
			})
		})
	}
}

func TestSkinSearchWithAChosenProfile(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b, s := skinBackend(t, "skin.arctic.horizon.2.1", addonSkinVariables)
		s.ShowWindow("1114", 0)

		skin, err := b.SkinSearch(t.Context(), "dune", "skin.arctic.horizon.2")
		require.NoError(t, err)
		assert.Equal(t, media.Skin{ID: "skin.arctic.horizon.2.1", Profile: "skin.arctic.horizon.2", Search: media.SkinSearchSupported}, skin)
		assert.Equal(t, []string{
			`Addons.ExecuteAddon {"addonid":"script.skinvariables","params":["run_executebuiltin=SetProperty(CustomSearchTerm,\"dune\",Home)||ActivateWindow(1114)"]}`,
		}, uiCalls(t, s))
	})
}

func TestSkinSearchFailures(t *testing.T) {
	t.Run("window never opens", func(t *testing.T) {
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

	t.Run("keyboard never opens", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			b, s := skinBackend(t, "skin.nimbus", addonNimbusHelper)
			s.ShowWindow("home", 0)
			s.ShowWindow("1121", 0)

			_, err := b.SkinSearch(t.Context(), "dune", "")
			require.ErrorIs(t, err, errWindowNotVisible)
			assert.NotContains(t, strings.Join(uiCalls(t, s), "\n"), "Input.SendText")
		})
	})

	for _, method := range []string{"Addons.ExecuteAddon", "XBMC.GetInfoBooleans", "GUI.ActivateWindow", "Input.SendText"} {
		t.Run(method+" fails", func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				b, s := skinBackend(t, "skin.nimbus", addonNimbusHelper)
				for _, w := range []string{"home", "1121", "virtualkeyboard"} {
					s.ShowWindow(w, 0)
				}

				s.Handle(method, func(json.RawMessage) (any, *fakekodi.Error) {
					return nil, &fakekodi.Error{Code: CodeInternalError, Message: "Internal error."}
				})

				_, err := b.SkinSearch(t.Context(), "dune", "")
				require.Error(t, err)
			})
		})
	}

	t.Run("each helper step fails", func(t *testing.T) {
		for _, fail := range []string{"mode=open_search_window", "mode=search_input"} {
			synctest.Test(t, func(t *testing.T) {
				b, s := skinBackend(t, "skin.nimbus", addonNimbusHelper)
				for _, w := range []string{"home", "1121", "virtualkeyboard"} {
					s.ShowWindow(w, 0)
				}

				s.Handle("Addons.ExecuteAddon", func(p json.RawMessage) (any, *fakekodi.Error) {
					if strings.Contains(string(p), fail) {
						return nil, &fakekodi.Error{Code: CodeInvalidParams, Message: "Invalid params."}
					}

					return "OK", nil
				})

				_, err := b.SkinSearch(t.Context(), "dune", "")
				require.Error(t, err, fail)
			})
		}
	})

	t.Run("home never shows", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			b, s := skinBackend(t, "skin.fentastic", addonFENtasticHelper)

			_, err := b.SkinSearch(t.Context(), "dune", "")
			require.ErrorIs(t, err, errWindowNotVisible)
			assert.Len(t, uiCalls(t, s), 1, "the helper isn't run")
		})
	})

	t.Run("the window opening step fails", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			b, s := skinBackend(t, "skin.arctic.fuse.3", addonSkinVariables)
			s.Handle("Addons.ExecuteAddon", func(json.RawMessage) (any, *fakekodi.Error) {
				return nil, &fakekodi.Error{Code: CodeInvalidParams, Message: "Invalid params."}
			})

			_, err := b.SkinSearch(t.Context(), "dune", "")
			require.Error(t, err)
			assert.Empty(t, s.CallsTo("XBMC.GetInfoBooleans"), "doesn't wait for a window that wasn't asked for")
		})
	})

	t.Run("cancelled while waiting", func(t *testing.T) {
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

	assert.Equal(t, []string{"skin.arctic.fuse.2", "skin.arctic.fuse.3", "skin.arctic.horizon.2", "skin.arctic.zephyr.2.resurrection.mod", "skin.fentastic", "skin.nimbus"}, SkinProfileIDs())
	assert.True(t, IsSkinProfile("skin.nimbus"))
	assert.False(t, IsSkinProfile("skin.estuary"), "runs Global Search")
	assert.False(t, IsSkinProfile("skin.unknown"))
	assert.False(t, IsSkinProfile(""))
}

func TestBuiltinParam(t *testing.T) {
	t.Parallel()

	tests := []struct{ query, want string }{
		{"dune", `"dune"`},
		{"dune, part two", `"dune, part two"`},
		{`say "hello"`, `"say \"hello\""`},
		{`back\slash`, `"back\\slash"`},
		{`ends with \`, `"ends with \\"`},
		{`\"`, `"\\\""`},
		{"it's", `"it's"`},
		{"'quoted'", `"'quoted'"`},
		{"(500) days of summer", `"(500) days of summer"`},
		{"closing) paren", `"closing) paren"`},
		{"tom & jerry", `"tom & jerry"`},
		{"x = y", `"x = y"`},
		{"a||b", `"a b"`},
		{"a|||b", `"a |b"`},
		{"a | b", `"a | b"`},
		{"$INFO[System.Time]", `"$INFO[System.Time]"`},
		{"  lots\tof \n space  ", `"lots of space"`},
		{"bell\x07and\x00nul", `"bell and nul"`},
		{"千と千尋の神隠し", `"千と千尋の神隠し"`},
		{"الرسالة", `"الرسالة"`},
		{"दिलवाले दुल्हनिया", `"दिलवाले दुल्हनिया"`},
		{"🎬 movie", `"🎬 movie"`},
		{"", `""`},
	}

	for _, tt := range tests {
		got := builtinParam(tt.query)
		assert.Equal(t, tt.want, got, "%q", tt.query)
		assert.Equal(t, []string{"x", cleanQuery(tt.query), "y"}, splitParams("x,"+got+",y"), "Kodi reads %q back as one parameter", tt.query)

		// Addons.ExecuteAddon quotes each argument of an array as Kodi's StringUtils::Paramify does, then RunAddon splits them again.
		args := []string{"run_executebuiltin=SetProperty(CustomSearchTerm," + got + ",Home)||ActivateWindow(1114)", "text=" + cleanQuery(tt.query)}
		assert.Equal(t, args, splitParams(paramify(args[0])+","+paramify(args[1])), "%q reaches the add-on as sent", tt.query)
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
