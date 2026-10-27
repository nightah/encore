package actions

import (
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
