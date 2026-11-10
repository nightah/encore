package kodi

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/nightah/encore/internal/backend/kodi/kodirpc"
	"github.com/nightah/encore/internal/core/media"
)

// skinProfiles are the skins whose search Encore knows, sorted by ID and matched exactly: forks may move windows and controls, so a fork needs its own entry or a profile chosen for it. Each entry names the skin files its steps were read from, so it can be checked again when the skin changes; the docs list the commits.
var skinProfiles = []skinProfile{
	// Aeon Nox: SiLVO ships script-globalsearch.xml.
	{ID: "skin.aeon.nox.silvo", Name: "Aeon Nox: SiLVO", Kind: skinSearchNative},
	// Aeon Tajo ships script-globalsearch.xml; its search dialog 1106 launches Global Search.
	{ID: "skin.aeon.tajo", Name: "Aeon Tajo", Kind: skinSearchNative},
	// Amber ships script-globalsearch.xml. Its own search dialog 1122 needs a builtin runner Amber doesn't depend on.
	{ID: "skin.amber", Name: "Amber", Kind: skinSearchNative},
	// Arctic Fuse 2: the standalone search window 1080i/Custom_1185_Search.xml, whose Search_Window_Def (1080i/Includes_Search.xml) types into edit 9099. Window 1105 is the search hub, which can be turned off.
	{
		ID: "skin.arctic.fuse.2", Name: "Arctic Fuse 2", Kind: skinSearchEditControl, Requires: []string{addonSkinVariables}, Window: "1185",
		run: typeIntoWindow("ActivateWindow(1185)", "1185", "9099"),
	},
	// Arctic Fuse 3: 1080i/Custom_1105_Search.xml, whose rows all read edit 3000 (1080i/Includes_Search.xml). ReplaceWindow matches the Home switcher's own Search tab (1080i/Includes_Home.xml), so Back and Home behave as usual.
	{
		ID: "skin.arctic.fuse.3", Name: "Arctic Fuse 3", Kind: skinSearchEditControl, Requires: []string{addonSkinVariables}, Window: "1105",
		run: typeIntoWindow("ReplaceWindow(1105)", "1105", "3000"),
	},
	// Arctic Zephyr 2: Resurrection Mod: 1080i/Custom_1138_Search.xml searches for Skin.String(SearchTerm) and goes back when it's empty, so the term is set before the window opens, as the skin's own "search for this" does.
	{
		ID: "skin.arctic.zephyr.2.resurrection.mod", Name: "Arctic Zephyr 2: Resurrection Mod", Kind: skinSearchBuiltins, Requires: []string{addonSkinVariables}, Window: "1138",
		run: func(ctx context.Context, b *Backend, q string) error {
			if err := b.skinCommand(ctx, "Skin.SetString(SearchTerm,"+builtinParam(q)+")"); err != nil {
				return err
			}

			if err := b.skinCommand(ctx, "ActivateWindow(1138)"); err != nil {
				return err
			}

			return b.waitVisible(ctx, "1138", windowWait)
		},
	},
	// Arctic Zephyr Mod depends on Global Search and ships script-globalsearch.xml.
	{ID: "skin.arctic.zephyr.mod", Name: "Arctic Zephyr Mod", Kind: skinSearchNative},
	// Estuary's search dialog (Custom_1107_SearchDialog.xml) launches Global Search, whose own default look is Estuary's.
	{ID: "skin.estuary", Name: "Estuary", Kind: skinSearchNative},
	// Estuary MOD V2 ships script-globalsearch.xml.
	{ID: "skin.estuary.modv2", Name: "Estuary MOD V2", Kind: skinSearchNative},
	// FENtastic: as Nimbus, through script.fentastic.helper. open_search_window reloads the skin when its search history is empty, so the window gets longer to open.
	{
		ID: "skin.fentastic", Name: "FENtastic", Kind: skinSearchKeyboard, Requires: []string{addonFENtasticHelper}, Window: "1121",
		run: helperKeyboard(addonFENtasticHelper, windowWaitReload),
	},
	// FTV depends on Global Search and ships script-globalsearch.xml.
	{ID: "skin.ftv", Name: "FTV", Kind: skinSearchNative},
	// Nimbus: script.nimbus.helper's resources/lib/modules/router.py and search_utils.py. open_search_window opens the results window 1121 only from Home; search_input takes no term and opens a keyboard.
	{
		ID: "skin.nimbus", Name: "Nimbus", Kind: skinSearchKeyboard, Requires: []string{addonNimbusHelper}, Window: "1121",
		run: helperKeyboard(addonNimbusHelper, windowWait),
	},
}

// SkinProfiles lists the skins whose search Encore knows, sorted by ID.
func SkinProfiles() []SkinProfile {
	out := make([]SkinProfile, 0, len(skinProfiles))

	for _, p := range skinProfiles {
		out = append(out, SkinProfile{ID: p.ID, Name: p.Name, Native: p.Kind == skinSearchNative, Requires: append([]string{}, p.Requires...), Window: p.Window, How: string(p.Kind)})
	}

	return out
}

// SkinProfileIDs lists the skin profiles IsSkinProfile accepts, sorted.
func SkinProfileIDs() []string {
	var ids []string

	for _, p := range skinProfiles {
		if p.Kind != skinSearchNative {
			ids = append(ids, p.ID)
		}
	}

	return ids
}

// IsSkinProfile reports whether id names a skin profile that can stand in for a skin without one: any but those that run Global Search.
func IsSkinProfile(id string) bool {
	p, ok := lookupSkinProfile(id)

	return ok && p.Kind != skinSearchNative
}

func lookupSkinProfile(id string) (skinProfile, bool) {
	i, ok := slices.BinarySearchFunc(skinProfiles, id, func(p skinProfile, id string) int { return strings.Compare(p.ID, id) })
	if !ok {
		return skinProfile{}, false
	}

	return skinProfiles[i], true
}

// Skin implements media.Backend. The skin is read from Kodi each time, since it can change at any moment.
func (b *Backend) Skin(ctx context.Context, profile string) (media.Skin, error) {
	skin, _, err := b.resolveSkin(ctx, profile)
	if err != nil {
		return skin, err
	}

	if skin.Name == "" {
		skin.Name = skin.ID

		r, err := b.c.AddonsGetAddonDetails(ctx, kodirpc.AddonsGetAddonDetailsParams{AddonID: skin.ID, Properties: kodirpc.AddonFields{"name"}})
		if err == nil && r.Addon.Name != "" {
			skin.Name = r.Addon.Name
		}
	}

	return skin, nil
}

// SkinSearch implements media.Backend.
func (b *Backend) SkinSearch(ctx context.Context, query, profile string) (media.Skin, error) {
	skin, p, err := b.resolveSkin(ctx, profile)
	if err != nil || skin.Search != media.SkinSearchSupported {
		return skin, err
	}

	if err := p.run(ctx, b, query); err != nil {
		return skin, fmt.Errorf("kodi: %s search: %w", p.ID, err)
	}

	return skin, nil
}

// resolveSkin reads the active skin and finds its profile: its own, else the one named by override, as long as it isn't a Global Search one. The profile is returned only when it can run.
func (b *Backend) resolveSkin(ctx context.Context, override string) (media.Skin, skinProfile, error) {
	r, err := b.c.SettingsGetSettingValue(ctx, kodirpc.SettingsGetSettingValueParams{Setting: skinSetting})
	if err != nil {
		return media.Skin{}, skinProfile{}, err
	}

	id, err := r.Value.AsString()
	if err != nil {
		return media.Skin{}, skinProfile{}, fmt.Errorf("kodi: reading %s: %w", skinSetting, err)
	}

	skin := media.Skin{ID: id, Search: media.SkinSearchNoProfile}

	p, ok := lookupSkinProfile(id)
	if ok {
		skin.Name = p.Name
	} else if IsSkinProfile(override) {
		p, ok = lookupSkinProfile(override)
	}

	if !ok {
		return skin, skinProfile{}, nil
	}

	skin.Profile = p.ID

	if p.Kind == skinSearchNative {
		skin.Search = media.SkinSearchNative

		return skin, skinProfile{}, nil
	}

	missing, err := b.missingAddons(ctx, p.Requires)
	if err != nil {
		return skin, skinProfile{}, err
	}

	if len(missing) > 0 {
		skin.Search, skin.Missing = media.SkinSearchMissingAddons, missing

		return skin, skinProfile{}, nil
	}

	skin.Search = media.SkinSearchSupported

	return skin, p, nil
}

// missingAddons returns the add-ons in ids that aren't installed and enabled.
func (b *Backend) missingAddons(ctx context.Context, ids []string) ([]string, error) {
	r, err := b.c.AddonsGetAddons(ctx, kodirpc.AddonsGetAddonsParams{Enabled: kodirpc.NewAddonsGetAddonsEnabledFromBool(true)})
	if err != nil {
		return nil, err
	}

	var missing []string

	for _, id := range ids {
		if !slices.ContainsFunc(r.Addons, func(a kodirpc.AddonDetails) bool { return a.AddonID == id }) {
			missing = append(missing, id)
		}
	}

	return missing, nil
}

// typeIntoWindow opens a skin's search window with the builtin open, focuses its edit control and types the query, which so never passes through Kodi's builtin parser.
func typeIntoWindow(open, window, control string) func(ctx context.Context, b *Backend, q string) error {
	return func(ctx context.Context, b *Backend, q string) error {
		if err := b.skinCommand(ctx, open); err != nil {
			return err
		}

		if err := b.waitVisible(ctx, window, windowWait); err != nil {
			return err
		}

		if err := b.skinCommand(ctx, "SetFocus("+control+")"); err != nil {
			return err
		}

		return b.sendTextDone(ctx, cleanQuery(q))
	}
}

// helperKeyboard opens the search window 1121 from Home through a Nimbus-style helper add-on, opens its keyboard and types the query, giving the window wait to open.
func helperKeyboard(helper string, wait time.Duration) func(ctx context.Context, b *Backend, q string) error {
	return func(ctx context.Context, b *Backend, q string) error {
		if err := b.activate(ctx, windowHomeName); err != nil {
			return err
		}

		if err := b.waitVisible(ctx, windowHomeName, windowWait); err != nil {
			return err
		}

		if err := b.runAddon(ctx, helper, "mode=open_search_window"); err != nil {
			return err
		}

		if err := b.waitVisible(ctx, "1121", wait); err != nil {
			return err
		}

		if err := b.runAddon(ctx, helper, "mode=search_input"); err != nil {
			return err
		}

		if err := b.waitVisible(ctx, windowKeyboard, windowWait); err != nil {
			return err
		}

		return b.sendTextDone(ctx, cleanQuery(q))
	}
}

// skinCommand runs a Kodi builtin, which JSON-RPC can't do itself, through the skin's copy of a script.skinvariables rules file whose last action is the builtin passed as its command. It goes through the add-on's plugin, listed with Files.GetDirectory so no window opens: Addons.ExecuteAddon would open that plugin rather than run its script, because Kodi's RunAddon prefers a plugin. The rules file closes any OSD first.
func (b *Backend) skinCommand(ctx context.Context, builtin string) error {
	dir := skinVariablesPlugin + "?info=run_executebuiltin&command=" + url.QueryEscape(builtin) + "&&" + skinCommandRules
	_, err := b.c.FilesGetDirectory(ctx, kodirpc.FilesGetDirectoryParams{Directory: dir})

	return err
}

// runAddon runs a script add-on with args as its argv. Kodi quotes each argument of an array as one builtin parameter, where an object's values would be split at commas. An add-on that is also a plugin opens as the plugin instead.
func (b *Backend) runAddon(ctx context.Context, id string, args ...string) error {
	_, err := b.c.AddonsExecuteAddon(ctx, kodirpc.AddonsExecuteAddonParams{AddonID: id, Params: kodirpc.NewAddonsExecuteAddonParams2FromArray(args)})

	return err
}

// sendTextDone types text into the focused field and confirms it, as pressing Done on the keyboard does.
func (b *Backend) sendTextDone(ctx context.Context, text string) error {
	_, err := b.c.InputSendText(ctx, kodirpc.InputSendTextParams{Text: text, Done: ptr(true)})

	return err
}

// waitVisible waits up to timeout for Kodi to show window, a window or dialog as Window.IsVisible takes it.
func (b *Backend) waitVisible(ctx context.Context, window string, timeout time.Duration) error {
	cond := "Window.IsVisible(" + window + ")"
	deadline := time.Now().Add(timeout)

	for {
		// Kodi's schema says the values are strings, but Kodi sends booleans.
		r, err := kodirpc.Call[kodirpc.XBMCGetInfoBooleansParams, map[string]bool](ctx, b.c.Caller, kodirpc.MethodXBMCGetInfoBooleans, kodirpc.XBMCGetInfoBooleansParams{Booleans: []string{cond}})
		if err != nil {
			return err
		}

		if r[cond] {
			return nil
		}

		if !time.Now().Add(windowPollInterval).Before(deadline) {
			return fmt.Errorf("%w: %s after %s", errWindowNotVisible, window, timeout)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(windowPollInterval):
		}
	}
}

// builtinParam quotes a query as one parameter of a Kodi builtin, so commas and parentheses in it neither split nor end the parameter. Kodi's parser drops the quotes and the backslashes escaping quotes and backslashes inside them.
func builtinParam(q string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(cleanQuery(q)) + `"`
}

// cleanQuery drops what a skin search can't carry: script.skinvariables' "||" builtin separator and control characters. Runs of whitespace become one space.
func cleanQuery(q string) string {
	q = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}

		return r
	}, q)

	for strings.Contains(q, "||") {
		q = strings.ReplaceAll(q, "||", " ")
	}

	return strings.Join(strings.Fields(q), " ")
}
