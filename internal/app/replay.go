package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/nightah/encore/internal/adapter/alexa"
	"github.com/nightah/encore/internal/core/actions"
	"github.com/nightah/encore/internal/core/media"
	"github.com/nightah/encore/internal/store"
)

// Calls returns the recorded commands in order.
func (d *DryRun) Calls() []Call {
	d.mu.Lock()
	defer d.mu.Unlock()

	return append([]Call(nil), d.calls...)
}

func (d *DryRun) record(method string, params map[string]any) {
	d.mu.Lock()
	d.calls = append(d.calls, Call{Method: method, Params: params})
	d.mu.Unlock()
}

var _ alexa.Targets = DryRunTargets{}

// Target implements alexa.Targets.
func (t DryRunTargets) Target(ctx context.Context, deviceID string) (actions.Target, error) {
	a := t.App

	dev, err := a.db.EchoDevices().GetByAlexaID(ctx, deviceID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return actions.Target{}, err
	}

	if target, ok := a.instanceTarget(ctx, dev.KodiInstanceID); ok {
		return t.dry(target), nil
	}

	def, err := a.db.KodiInstances().Default(ctx)

	switch {
	case errors.Is(err, store.ErrNotFound):
		return actions.Target{}, alexa.ErrUnlinked
	case err != nil:
		return actions.Target{}, err
	}

	if target, ok := a.instanceTarget(ctx, def.ID); ok {
		return t.dry(target), nil
	}

	return actions.Target{}, alexa.ErrUnlinked
}

// TargetForRoom implements alexa.Targets.
func (t DryRunTargets) TargetForRoom(ctx context.Context, deviceID, spoken string) (actions.Target, error) {
	a := t.App

	room, err := a.db.Rooms().Resolve(ctx, spoken)

	if rest, ok := cutArticle(spoken); ok && errors.Is(err, store.ErrNotFound) {
		room, err = a.db.Rooms().Resolve(ctx, rest)
	}

	switch {
	case errors.Is(err, store.ErrNotFound):
		return actions.Target{}, fmt.Errorf("%w: “%s”", alexa.ErrUnknownRoom, spoken)
	case err != nil:
		return actions.Target{}, err
	}

	if target, ok := a.instanceTarget(ctx, room.KodiInstanceID); ok && room.KodiInstanceID != 0 {
		return t.dry(target), nil
	}

	return t.Target(ctx, deviceID)
}

// DeviceName implements alexa.Targets without recording the device as seen.
func (t DryRunTargets) DeviceName(ctx context.Context, deviceID string) (string, error) {
	dev, err := t.App.db.EchoDevices().GetByAlexaID(ctx, deviceID)
	if errors.Is(err, store.ErrNotFound) {
		return "", nil
	}

	return dev.DisplayName, err
}

// dry swaps a target's backend for one that records commands instead of sending them.
func (t DryRunTargets) dry(target actions.Target) actions.Target {
	target.Backend = dryBackend{Backend: target.Backend, run: t.Run}
	target.Wake = nil

	return target
}

func (b dryBackend) Play(_ context.Context, ref media.Ref, opts media.PlayOptions) error {
	b.run.record("Player.Open", map[string]any{"kind": ref.Kind, "id": ref.ID, "resume": opts.Resume, "shuffle": opts.Shuffle})

	return nil
}

func (b dryBackend) Transport(_ context.Context, cmd media.Transport) error {
	b.run.record("Player.Transport", map[string]any{"command": cmd})

	return nil
}

func (b dryBackend) Seek(_ context.Context, s media.Seek) error {
	params := map[string]any{}

	switch {
	case s.To != nil:
		params["to_s"] = s.To.Seconds()
	case s.Step != "":
		params["step"] = s.Step
	default:
		params["by_s"] = s.By.Seconds()
	}

	b.run.record("Player.Seek", params)

	return nil
}

func (b dryBackend) Volume(_ context.Context, c media.VolumeChange) (media.VolumeState, error) {
	params := map[string]any{}

	switch {
	case c.Set != nil:
		params["set"] = *c.Set
	case c.Mute != nil:
		params["mute"] = *c.Mute
	case c.ToggleMute:
		params["toggle_mute"] = true
	default:
		params["step"] = c.Step
	}

	b.run.record("Application.SetVolume", params)

	state := media.VolumeState{Level: 50}
	if c.Set != nil {
		state.Level = *c.Set
	}

	return state, nil
}

func (b dryBackend) Queue(_ context.Context, refs []media.Ref) error {
	b.run.record("Playlist.Add", map[string]any{"items": refs})

	return nil
}

func (b dryBackend) PlayFile(_ context.Context, path string) error {
	b.run.record("Player.Open", map[string]any{"file": path})

	return nil
}

func (b dryBackend) PartyMode(context.Context) error {
	b.run.record("Player.Open", map[string]any{"partymode": true})

	return nil
}

func (b dryBackend) SetShuffle(_ context.Context, on bool) error {
	b.run.record("Player.SetShuffle", map[string]any{"shuffle": on})

	return nil
}

func (b dryBackend) SetRepeat(_ context.Context, mode media.Repeat) (media.Repeat, error) {
	b.run.record("Player.SetRepeat", map[string]any{"repeat": mode})

	return mode, nil
}

func (b dryBackend) Picture(_ context.Context, cmd media.PictureCommand) error {
	b.run.record("Player.Picture", map[string]any{"command": cmd})

	return nil
}

func (b dryBackend) SetSubtitle(_ context.Context, c media.StreamChange) error {
	b.run.record("Player.SetSubtitle", map[string]any{"change": c})

	return nil
}

func (b dryBackend) SetAudioStream(_ context.Context, c media.StreamChange) error {
	b.run.record("Player.SetAudioStream", map[string]any{"change": c})

	return nil
}

func (b dryBackend) Input(_ context.Context, action media.InputAction) error {
	b.run.record("Input.ExecuteAction", map[string]any{"action": action})

	return nil
}

func (b dryBackend) SendText(_ context.Context, text string) error {
	b.run.record("Input.SendText", map[string]any{"text": text})

	return nil
}

func (b dryBackend) ShowView(_ context.Context, v media.View) error {
	b.run.record("GUI.ActivateWindow", map[string]any{"view": v})

	return nil
}

func (b dryBackend) OpenFavourite(_ context.Context, f media.Favourite) error {
	b.run.record("Favourites.Open", map[string]any{"title": f.Title})

	return nil
}

func (b dryBackend) ExecuteAddon(_ context.Context, id string, params map[string]string) error {
	b.run.record("Addons.ExecuteAddon", map[string]any{"addonid": id, "params": params})

	return nil
}

func (b dryBackend) System(_ context.Context, cmd media.SystemCommand) error {
	b.run.record("System", map[string]any{"command": cmd})

	return nil
}

func (b dryBackend) ScanLibrary(_ context.Context, db media.Database) error {
	b.run.record("Library.Scan", map[string]any{"database": db})

	return nil
}

func (b dryBackend) CleanLibrary(_ context.Context, db media.Database) error {
	b.run.record("Library.Clean", map[string]any{"database": db})

	return nil
}
