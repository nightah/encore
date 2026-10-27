package testkit

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/nightah/encore/internal/core/media"
)

// SetStreams sets what Streams reports.
func (b *Backend) SetStreams(s media.Streams) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.streams = s
}

// SetFavourites sets what Favourites reports.
func (b *Backend) SetFavourites(f ...media.Favourite) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.favourites = f
}

// SetAddons sets what Addons reports.
func (b *Backend) SetAddons(a ...media.Addon) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.addons = a
}

// SetClock sets what Clock reports.
func (b *Backend) SetClock(t time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.clock = t
}

// Queue implements media.Backend; it plays the first ref.
func (b *Backend) Queue(_ context.Context, refs []media.Ref) error {
	ids := make([]string, len(refs))
	for i, r := range refs {
		ids[i] = fmt.Sprintf("%s/%d", r.Kind, r.ID)
	}

	if err := b.record("Queue %s", strings.Join(ids, ",")); err != nil {
		return err
	}

	if len(refs) == 0 {
		return media.ErrNotFound
	}

	for _, i := range b.filter(func(i media.Item) bool { return i.Ref() == refs[0] }) {
		b.SetPlaying(&media.NowPlaying{Item: i, Duration: i.Runtime})
	}

	return nil
}

// PlayFile implements media.Backend.
func (b *Backend) PlayFile(_ context.Context, path string) error {
	return b.record("PlayFile %s", path)
}

// PartyMode implements media.Backend.
func (b *Backend) PartyMode(context.Context) error {
	return b.record("PartyMode")
}

// playingErr reports ErrNothingPlaying when nothing is playing, like a real player command.
func (b *Backend) playingErr() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.playing == nil {
		return media.ErrNothingPlaying
	}

	return nil
}

// SetShuffle implements media.Backend.
func (b *Backend) SetShuffle(_ context.Context, on bool) error {
	if err := b.record("SetShuffle %t", on); err != nil {
		return err
	}

	return b.playingErr()
}

// SetRepeat implements media.Backend.
func (b *Backend) SetRepeat(_ context.Context, mode media.Repeat) (media.Repeat, error) {
	if err := b.record("SetRepeat %s", mode); err != nil {
		return "", err
	}

	if err := b.playingErr(); err != nil {
		return "", err
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	if mode == media.RepeatCycle {
		next := map[media.Repeat]media.Repeat{media.RepeatOff: media.RepeatOne, media.RepeatOne: media.RepeatAll, media.RepeatAll: media.RepeatOff}
		mode = next[b.repeat]
		if mode == "" {
			mode = media.RepeatOne
		}
	}

	b.repeat = mode

	return mode, nil
}

// Picture implements media.Backend.
func (b *Backend) Picture(_ context.Context, cmd media.PictureCommand) error {
	if err := b.record("Picture %s", cmd); err != nil {
		return err
	}

	return b.playingErr()
}

// Streams implements media.Backend.
func (b *Backend) Streams(context.Context) (media.Streams, error) {
	if err := b.record("Streams"); err != nil {
		return media.Streams{}, err
	}

	if err := b.playingErr(); err != nil {
		return media.Streams{}, err
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	return b.streams, nil
}

// SetSubtitle implements media.Backend. Steps and indexes move the current subtitle as Kodi would.
func (b *Backend) SetSubtitle(_ context.Context, c media.StreamChange) error {
	if err := b.record("SetSubtitle %s", describeStream(c)); err != nil {
		return err
	}

	if err := b.playingErr(); err != nil {
		return err
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	s := &b.streams

	switch c.Step {
	case media.StreamOn:
		s.SubtitlesOn = true
	case media.StreamOff:
		s.SubtitlesOn = false
	default:
		s.CurrentSubtitle = step(s.Subtitles, s.CurrentSubtitle, c)
		s.SubtitlesOn = true
	}

	return nil
}

// SetAudioStream implements media.Backend.
func (b *Backend) SetAudioStream(_ context.Context, c media.StreamChange) error {
	if err := b.record("SetAudioStream %s", describeStream(c)); err != nil {
		return err
	}

	if err := b.playingErr(); err != nil {
		return err
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	b.streams.CurrentAudio = step(b.streams.Audio, b.streams.CurrentAudio, c)

	return nil
}

func step(streams []media.Stream, cur *media.Stream, c media.StreamChange) *media.Stream {
	if len(streams) == 0 {
		return cur
	}

	i := 0
	if cur != nil {
		i = slices.IndexFunc(streams, func(s media.Stream) bool { return s.Index == cur.Index })
	}

	switch c.Step {
	case media.StreamNext:
		i = (i + 1) % len(streams)
	case media.StreamPrevious:
		i = (i - 1 + len(streams)) % len(streams)
	default:
		i = slices.IndexFunc(streams, func(s media.Stream) bool { return s.Index == c.Index })
	}

	if i < 0 {
		return cur
	}

	s := streams[i]

	return &s
}

func describeStream(c media.StreamChange) string {
	if c.Step != "" {
		return string(c.Step)
	}

	return fmt.Sprintf("index %d", c.Index)
}

// Input implements media.Backend.
func (b *Backend) Input(_ context.Context, action media.InputAction) error {
	return b.record("Input %s", action)
}

// SendText implements media.Backend.
func (b *Backend) SendText(_ context.Context, text string) error {
	return b.record("SendText %s", text)
}

// ShowView implements media.Backend.
func (b *Backend) ShowView(_ context.Context, v media.View) error {
	desc := string(v.Section)
	if v.Genre != "" {
		desc += " genre " + v.Genre
	}

	if v.Section == media.SectionPlaylist {
		desc += fmt.Sprintf(" %s/%d", v.Playlist.Kind, v.Playlist.ID)
	}

	return b.record("ShowView %s", desc)
}

// Favourites implements media.Backend.
func (b *Backend) Favourites(context.Context) ([]media.Favourite, error) {
	if err := b.record("Favourites"); err != nil {
		return nil, err
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	return slices.Clone(b.favourites), nil
}

// OpenFavourite implements media.Backend.
func (b *Backend) OpenFavourite(_ context.Context, f media.Favourite) error {
	return b.record("OpenFavourite %s", f.Title)
}

// Addons implements media.Backend.
func (b *Backend) Addons(context.Context) ([]media.Addon, error) {
	if err := b.record("Addons"); err != nil {
		return nil, err
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	return slices.Clone(b.addons), nil
}

// ExecuteAddon implements media.Backend.
func (b *Backend) ExecuteAddon(_ context.Context, id string, params map[string]string) error {
	desc := id

	for _, k := range slices.Sorted(maps.Keys(params)) {
		desc += " " + k + "=" + params[k]
	}

	return b.record("ExecuteAddon %s", desc)
}

// Clock implements media.Backend.
func (b *Backend) Clock(context.Context) (time.Time, error) {
	if err := b.record("Clock"); err != nil {
		return time.Time{}, err
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	return b.clock, nil
}

// System implements media.Backend.
func (b *Backend) System(_ context.Context, cmd media.SystemCommand) error {
	return b.record("System %s", cmd)
}

// ScanLibrary implements media.Backend.
func (b *Backend) ScanLibrary(_ context.Context, db media.Database) error {
	return b.record("ScanLibrary %s", db)
}

// CleanLibrary implements media.Backend.
func (b *Backend) CleanLibrary(_ context.Context, db media.Database) error {
	return b.record("CleanLibrary %s", db)
}
