package actions

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/core/media"
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

	assert.Equal(t, "addon.no_global_search", svc.GlobalSearch(t.Context(), target, "dune").Say.ID)

	backend.SetAddons(media.Addon{ID: "plugin.video.youtube", Name: "YouTube"}, media.Addon{ID: "script.globalsearch", Name: "Global Search"})

	o := svc.LaunchAddon(t.Context(), target, "you tube")
	assert.Equal(t, "addon.opening", o.Say.ID)
	assert.Equal(t, "YouTube", o.Say.Params["addon"])

	assert.Equal(t, "addon.not_found", svc.LaunchAddon(t.Context(), target, "netflix").Say.ID)

	o = svc.GlobalSearch(t.Context(), target, "dune")
	assert.Equal(t, "addon.searching", o.Say.ID)

	assert.Contains(t, backend.Calls(), "ExecuteAddon plugin.video.youtube")
	assert.Contains(t, backend.Calls(), "ExecuteAddon script.globalsearch searchstring=dune")
}

func TestAddonNamesMustBeSpoken(t *testing.T) {
	target, backend, _ := newTarget()
	withAddons(backend)
	svc := newService()

	assert.Equal(t, "remote.no_text", svc.GlobalSearch(t.Context(), target, " \t").Say.ID)
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

func TestGlobalSearchModeRunsGlobalSearchOnly(t *testing.T) {
	for _, mode := range []media.SearchMode{"", media.SearchModeGlobal} {
		target, backend, _ := newTarget()
		withAddons(backend)
		backend.SetSkin(media.Skin{ID: "skin.arctic.fuse.3", Search: media.SkinSearchSupported}, nil)
		target.SearchMode = mode

		o := newService().GlobalSearch(t.Context(), target, "dune")
		assert.Equal(t, "addon.searching", o.Say.ID)
		assert.Empty(t, o.Note)
		assert.Equal(t, []string{"Addons", "ExecuteAddon script.globalsearch searchstring=dune"}, backend.Calls(), "mode %q leaves the skin alone", mode)
	}
}

func TestSkinSearch(t *testing.T) {
	target, backend, _ := newTarget()
	backend.SetSkin(media.Skin{ID: "skin.arctic.fuse.3", Profile: "skin.arctic.fuse.3", Search: media.SkinSearchSupported}, nil)
	target.SearchMode = media.SearchModeSkin
	target.SkinProfile = "skin.arctic.fuse.2"

	o := newService().GlobalSearch(t.Context(), target, "dune")
	assert.Equal(t, "addon.searching", o.Say.ID)
	assert.Equal(t, "dune", o.Say.Params["query"])
	assert.True(t, o.EndSession)
	require.NoError(t, o.Err)
	assert.Equal(t, "skin search: opened skin.arctic.fuse.3's search", o.Note)
	assert.Equal(t, []string{"SkinSearch dune profile=skin.arctic.fuse.2"}, backend.Calls(), "Global Search isn't needed")

	backend.SetSkin(media.Skin{ID: "skin.af2.fork", Profile: "skin.arctic.fuse.2", Search: media.SkinSearchSupported}, nil)
	o = newService().GlobalSearch(t.Context(), target, "dune")
	assert.Equal(t, "addon.searching", o.Say.ID)
	assert.Equal(t, "skin search: opened skin.af2.fork's search as skin.arctic.fuse.2", o.Note)
}

func TestSkinSearchFallsBackToGlobalSearch(t *testing.T) {
	tests := []struct {
		name string
		skin media.Skin
		err  error
		fail bool
		note string
	}{
		{
			name: "no profile", skin: media.Skin{ID: "skin.copacetic", Search: media.SkinSearchNoProfile},
			note: "skin search: skin.copacetic has no profile; used Global Search",
		},
		{
			name: "skins Global Search", skin: media.Skin{ID: "skin.estuary", Profile: "skin.estuary", Search: media.SkinSearchNative},
			note: "skin search: skin.estuary skins Global Search; used Global Search",
		},
		{
			name: "missing add-ons", skin: media.Skin{ID: "skin.arctic.fuse.3", Profile: "skin.arctic.fuse.3", Search: media.SkinSearchMissingAddons, Missing: []string{"script.skinvariables"}},
			note: "skin search: skin.arctic.fuse.3 needs script.skinvariables; used Global Search",
		},
		{
			name: "step failed", skin: media.Skin{ID: "skin.arctic.fuse.3", Profile: "skin.arctic.fuse.3", Search: media.SkinSearchSupported}, err: fmt.Errorf("kodi: skin.arctic.fuse.3 search: %w", errBoom),
			note: "skin search: skin.arctic.fuse.3 failed: kodi: skin.arctic.fuse.3 search: boom; used Global Search",
		},
		{
			name: "skin unreadable", fail: true,
			note: "skin search: couldn't read the skin: boom; used Global Search",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target, backend, _ := newTarget()
			withAddons(backend)
			backend.SetSkin(tt.skin, tt.err)
			target.SearchMode = media.SearchModeSkin

			if tt.fail {
				target, _ = failing(target, backend, errBoom, "SkinSearch")
			}

			o := newService().GlobalSearch(t.Context(), target, "dune")
			assert.Equal(t, "addon.searching", o.Say.ID)
			require.NoError(t, o.Err)
			assert.Equal(t, tt.note, o.Note)

			calls := backend.Calls()
			assert.Equal(t, []string{"Addons", "ExecuteAddon script.globalsearch searchstring=dune"}, calls[len(calls)-2:], "Global Search runs once")
		})
	}
}

func TestSkinSearchWithoutGlobalSearch(t *testing.T) {
	target, backend, _ := newTarget()
	backend.SetSkin(media.Skin{ID: "skin.copacetic", Search: media.SkinSearchNoProfile}, nil)
	target.SearchMode = media.SearchModeSkin

	o := newService().GlobalSearch(t.Context(), target, "dune")
	assert.Equal(t, "addon.no_global_search", o.Say.ID)
	assert.Equal(t, "skin search: skin.copacetic has no profile; used Global Search", o.Note)

	backend.SetSkin(media.Skin{ID: "skin.arctic.fuse.3", Profile: "skin.arctic.fuse.3", Search: media.SkinSearchSupported}, nil)
	assert.Equal(t, "addon.searching", newService().GlobalSearch(t.Context(), target, "dune").Say.ID, "a skin search doesn't need Global Search")

	assert.Equal(t, "remote.no_text", newService().GlobalSearch(t.Context(), target, "  ").Say.ID)
}
