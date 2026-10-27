package app

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/core/media"
	"github.com/nightah/encore/internal/testkit"
)

// TestDryBackendSendsNoWrites guards against a write method added to media.Backend reaching Kodi during a replay.
func TestDryBackendSendsNoWrites(t *testing.T) {
	kodi := testkit.NewBackend(testkit.SampleLibrary()...)
	run := &DryRun{}
	b := dryBackend{Backend: kodi, run: run}
	ctx := t.Context()
	ref := media.Ref{Kind: media.KindMovie, ID: 1}
	level := 30

	writes := []func() error{
		func() error { return b.Play(ctx, ref, media.PlayOptions{}) },
		func() error { return b.Queue(ctx, []media.Ref{ref}) },
		func() error { return b.PlayFile(ctx, "/media/a.mkv") },
		func() error { return b.PartyMode(ctx) },
		func() error { return b.Transport(ctx, media.Transport("pause")) },
		func() error { return b.Seek(ctx, media.Seek{Step: "smallforward"}) },
		func() error { _, err := b.Volume(ctx, media.VolumeChange{Set: &level}); return err },
		func() error { return b.SetShuffle(ctx, true) },
		func() error { _, err := b.SetRepeat(ctx, media.Repeat("all")); return err },
		func() error { return b.Picture(ctx, media.PictureCommand("zoomin")) },
		func() error { return b.SetSubtitle(ctx, media.StreamChange{}) },
		func() error { return b.SetAudioStream(ctx, media.StreamChange{}) },
		func() error { return b.Input(ctx, media.InputAction("select")) },
		func() error { return b.SendText(ctx, "dune") },
		func() error { return b.ShowView(ctx, media.View{}) },
		func() error { return b.OpenFavourite(ctx, media.Favourite{Title: "Radio"}) },
		func() error { return b.ExecuteAddon(ctx, "plugin.video.youtube", nil) },
		func() error { return b.System(ctx, media.SystemCommand("shutdown")) },
		func() error { return b.ScanLibrary(ctx, media.Database("video")) },
		func() error { return b.CleanLibrary(ctx, media.Database("video")) },
	}

	for _, w := range writes {
		require.NoError(t, w())
	}

	assert.Empty(t, kodi.Calls(), "a dry run must not send commands")
	assert.Len(t, run.Calls(), len(writes))
}
