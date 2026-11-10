package actions

import (
	"context"
	"fmt"
	"strings"

	"github.com/nightah/encore/internal/core/media"
)

// Power asks for confirmation before shutting down, rebooting, hibernating or suspending.
func (s *Service) Power(_ context.Context, t Target, cmd media.SystemCommand) Outcome {
	msg := say("power.confirm_"+string(cmd), map[string]string{"kodi": t.Name})
	if cmd == media.SystemShutdown && t.ShutdownQuits {
		msg = say("power.confirm_quit", map[string]string{"kodi": t.Name})
	}

	return Outcome{Say: msg, Reprompt: &msg, Followup: &Followup{Confirm: cmd}}
}

// ConfirmPower runs a confirmed power command. "Shut down" quits Kodi instead when the target is set up that way.
func (s *Service) ConfirmPower(ctx context.Context, t Target, cmd media.SystemCommand) Outcome {
	if cmd == media.SystemShutdown && t.ShutdownQuits {
		cmd = media.SystemQuit
	}

	if err := t.Backend.System(ctx, cmd); err != nil {
		return failure(t, err)
	}

	return done(say("power."+string(cmd), map[string]string{"kodi": t.Name}))
}

// Eject opens the disc tray.
func (s *Service) Eject(ctx context.Context, t Target) Outcome {
	if err := t.Backend.System(ctx, media.SystemEject); err != nil {
		return failure(t, err)
	}

	return done(say("power.eject", nil))
}

// UpdateLibrary scans for new content, or cleans out missing content, in the given databases (both when none are given).
func (s *Service) UpdateLibrary(ctx context.Context, t Target, clean bool, dbs ...media.Database) Outcome {
	if len(dbs) == 0 {
		dbs = []media.Database{media.DatabaseVideo, media.DatabaseMusic}
	}

	for _, db := range dbs {
		var err error
		if clean {
			err = t.Backend.CleanLibrary(ctx, db)
		} else {
			err = t.Backend.ScanLibrary(ctx, db)
		}

		if err != nil {
			return failure(t, err)
		}
	}

	what := "both"
	if len(dbs) == 1 {
		what = string(dbs[0])
	}

	verb := "scan"
	if clean {
		verb = "clean"
	}

	return done(say("library."+verb+"_"+what, nil))
}

// LaunchAddon starts an add-on by name.
func (s *Service) LaunchAddon(ctx context.Context, t Target, name string) Outcome {
	addons, err := t.Backend.Addons(ctx)
	if err != nil {
		return failure(t, err)
	}

	names := make([]string, len(addons))
	for i := range addons {
		names[i] = addons[i].Name
	}

	i, ok := bestName(t.language(), names, name)
	if !ok {
		return done(say("addon.not_found", map[string]string{"addon": name}))
	}

	if err := t.Backend.ExecuteAddon(ctx, addons[i].ID, nil); err != nil {
		return failure(t, err)
	}

	return done(say("addon.opening", map[string]string{"addon": addons[i].Name}))
}

// GlobalSearch searches for query: in the skin's own search when the target is set up for it and Encore knows how, else with the Global Search add-on, when it's installed.
func (s *Service) GlobalSearch(ctx context.Context, t Target, query string) Outcome {
	if strings.TrimSpace(query) == "" {
		return done(say("remote.no_text", nil))
	}

	if t.SearchMode != media.SearchModeSkin {
		return s.globalSearch(ctx, t, query)
	}

	skin, err := t.Backend.SkinSearch(ctx, query, t.SkinProfile)
	if err == nil && skin.Search == media.SkinSearchSupported {
		o := done(say("addon.searching", map[string]string{"query": query}))
		o.Note = "skin search: " + skinUsed(skin)

		return o
	}

	o := s.globalSearch(ctx, t, query)
	o.Note = "skin search: " + skinFallback(skin, err) + "; used Global Search"

	return o
}

func (s *Service) globalSearch(ctx context.Context, t Target, query string) Outcome {
	addons, err := t.Backend.Addons(ctx)
	if err != nil {
		return failure(t, err)
	}

	installed := false

	for _, a := range addons {
		installed = installed || a.ID == globalSearchAddon
	}

	if !installed {
		return done(say("addon.no_global_search", nil))
	}

	if err := t.Backend.ExecuteAddon(ctx, globalSearchAddon, map[string]string{"searchstring": query}); err != nil {
		return failure(t, err)
	}

	return done(say("addon.searching", map[string]string{"query": query}))
}

// skinUsed describes the skin search that ran, for the request log.
func skinUsed(skin media.Skin) string {
	if skin.Profile != skin.ID {
		return fmt.Sprintf("opened %s's search as %s", skin.ID, skin.Profile)
	}

	return "opened " + skin.ID + "'s search"
}

// skinFallback says why a skin search didn't run, for the request log.
func skinFallback(skin media.Skin, err error) string {
	switch {
	case err != nil && skin.ID == "":
		return "couldn't read the skin: " + err.Error()
	case err != nil:
		return skin.ID + " failed: " + err.Error()
	case skin.Search == media.SkinSearchNative:
		return skin.ID + " skins Global Search"
	case skin.Search == media.SkinSearchMissingAddons:
		return skin.ID + " needs " + strings.Join(skin.Missing, ", ")
	default:
		return skin.ID + " has no profile"
	}
}
