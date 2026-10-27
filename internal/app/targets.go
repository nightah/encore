package app

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/nightah/encore/internal/adapter/alexa"
	"github.com/nightah/encore/internal/config"
	"github.com/nightah/encore/internal/core/actions"
	"github.com/nightah/encore/internal/core/media"
	"github.com/nightah/encore/internal/store"
)

var _ alexa.Targets = (*App)(nil)

// Target returns the Kodi instance an Echo device controls: its linked instance, else the default for unlinked devices. Every call records the device as seen, which is how new Echos appear in the web interface.
func (a *App) Target(ctx context.Context, deviceID string) (actions.Target, error) {
	dev, err := a.db.EchoDevices().Seen(ctx, deviceID)
	if err != nil {
		return actions.Target{}, err
	}

	if t, ok := a.instanceTarget(ctx, dev.KodiInstanceID); ok {
		return t, nil
	}

	def, err := a.db.KodiInstances().Default(ctx)

	switch {
	case errors.Is(err, store.ErrNotFound):
		return actions.Target{}, alexa.ErrUnlinked
	case err != nil:
		return actions.Target{}, err
	}

	if t, ok := a.instanceTarget(ctx, def.ID); ok {
		return t, nil
	}

	return actions.Target{}, alexa.ErrUnlinked
}

// TargetForRoom returns the Kodi instance of a room named in a request ("play Dune in the bedroom"), matching room names and aliases. A room without a Kodi instance falls back to the device's own target; a name that matches no room is alexa.ErrUnknownRoom.
func (a *App) TargetForRoom(ctx context.Context, deviceID, spoken string) (actions.Target, error) {
	room, err := a.db.Rooms().Resolve(ctx, spoken)

	// Alexa sometimes keeps the article: "the bedroom".
	if rest, ok := cutArticle(spoken); ok && errors.Is(err, store.ErrNotFound) {
		room, err = a.db.Rooms().Resolve(ctx, rest)
	}

	switch {
	case errors.Is(err, store.ErrNotFound):
		return actions.Target{}, fmt.Errorf("%w: “%s”", alexa.ErrUnknownRoom, spoken)
	case err != nil:
		return actions.Target{}, err
	}

	if room.KodiInstanceID != 0 {
		// The device is still recorded as seen, as Target does.
		if _, err := a.db.EchoDevices().Seen(ctx, deviceID); err != nil {
			return actions.Target{}, err
		}

		if t, ok := a.instanceTarget(ctx, room.KodiInstanceID); ok {
			return t, nil
		}
	}

	return a.Target(ctx, deviceID)
}

// DeviceName returns the Echo's name in Encore, "" if it has none yet, and records it as seen so a new Echo shows up in the web interface.
func (a *App) DeviceName(ctx context.Context, deviceID string) (string, error) {
	dev, err := a.db.EchoDevices().Seen(ctx, deviceID)
	if err != nil {
		return "", err
	}

	return dev.DisplayName, nil
}

func (a *App) instanceTarget(ctx context.Context, id int64) (actions.Target, bool) {
	a.mu.RLock()
	inst, ok := a.instances[id]

	var t actions.Target
	if ok {
		t = inst.target()
	}
	a.mu.RUnlock()

	if !ok {
		return actions.Target{}, false
	}

	voice := a.voiceSettings(ctx)

	t.Language = inst.language()
	t.QueueLimit = voice.QueueLimit
	t.ShutdownQuits = voice.ShutdownQuits
	t.DeepSearch = voice.DeepSearch
	t.ConfirmSound = voice.ConfirmSound

	for _, c := range media.Contents {
		if !slices.Contains(voice.Content, string(c)) {
			t.Disabled = append(t.Disabled, c)
		}
	}

	if q := inst.Record.ShutdownQuits; q != nil {
		t.ShutdownQuits = *q
	}

	return t, true
}

func cutArticle(spoken string) (string, bool) {
	f := strings.Fields(spoken)
	if len(f) < 2 || !strings.EqualFold(f[0], "the") {
		return "", false
	}

	return strings.Join(f[1:], " "), true
}

// SetSettings gives the app the effective configuration: config where it sets a key, otherwise what was saved in the web interface. Without it the startup configuration is used.
func (a *App) SetSettings(effective func(context.Context) (*config.Config, error)) {
	a.mu.Lock()
	a.effective = effective
	a.mu.Unlock()

	// Libraries made before now used the startup configuration's content groups; the web interface's choice applies from the first sync.
	a.ApplyContent(context.Background())
}

// voiceSettings reads the effective configuration, not the startup one.
func (a *App) voiceSettings(ctx context.Context) config.Alexa {
	a.mu.RLock()
	effective := a.effective
	a.mu.RUnlock()

	if effective == nil {
		return a.cfg.Alexa
	}

	cfg, err := effective(ctx)
	if err != nil {
		a.log.Warn("reading settings; using the startup configuration", "error", err)

		return a.cfg.Alexa
	}

	return cfg.Alexa
}
