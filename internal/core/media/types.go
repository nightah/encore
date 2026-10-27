package media

import (
	"context"
	"time"
)

// Kind is a type of library item.
type Kind string

// Content is a group of kinds the skill can be set up to play: movies, TV or music (which takes music videos along).
type Content string

// Ref identifies an item within one backend instance.
type Ref struct {
	Kind Kind `json:"kind"`
	ID   int  `json:"id"`
}

// Item is one library entry. Fields that don't apply to a kind are zero.
type Item struct {
	Kind          Kind
	ID            int
	Title         string
	SortTitle     string
	OriginalTitle string
	Year          int
	Genres        []string

	// Episodes.
	ShowID    int
	ShowTitle string
	Season    int
	Episode   int

	// Music.
	Artist string
	Album  string

	// Movies: the trailer as a path or URL the backend can play, and the collection ("set") the movie belongs to.
	Trailer  string
	SetID    int
	SetTitle string

	PlayCount  int
	Resume     time.Duration
	Runtime    time.Duration
	DateAdded  time.Time
	LastPlayed time.Time

	// Art holds the backend's image references; for Kodi, image:// URLs fetched through its web server.
	Art Artwork
}

// Artwork is an item's images as the backend refers to them.
type Artwork struct {
	Poster string
	Thumb  string
	Fanart string
}

// PlayOptions modify Play.
type PlayOptions struct {
	// Resume continues from the saved position when there is one.
	Resume bool
	// Shuffle randomises containers such as shows, artists, albums and playlists. The backend shuffles them itself rather than trusting the media centre to, since Kodi ignores shuffling for long lists.
	Shuffle bool
	// Limit caps how many items a shuffled container queues; zero means no cap.
	Limit int
}

// Transport is a playback control command.
type Transport string

// SeekStep is a relative seek in the backend's configured step sizes.
type SeekStep string

// Seek moves the playhead. Exactly one field is set.
type Seek struct {
	// By seeks relative to the current position; negative goes back.
	By time.Duration
	// To seeks to an absolute position.
	To *time.Duration
	// Step uses the backend's small or big step.
	Step SeekStep
}

// VolumeChange adjusts volume. Exactly one field is set.
type VolumeChange struct {
	// Set is an absolute level from 0 to 100.
	Set *int
	// Step is a relative change, e.g. +10 or -10.
	Step int
	// Mute sets the mute state; ToggleMute flips it.
	Mute       *bool
	ToggleMute bool
}

// VolumeState is the volume after a change.
type VolumeState struct {
	Level int
	Muted bool
}

// NowPlaying describes the active player.
type NowPlaying struct {
	Item     Item
	Position time.Duration
	Duration time.Duration
	Paused   bool
}

// Info describes a backend instance.
type Info struct {
	Name       string
	Version    string
	APIVersion string
}

// EventKind classifies a backend event.
type EventKind string

// Event is a change pushed by the backend. Ref is set for item and playback events.
type Event struct {
	Kind EventKind
	Ref  Ref
}

// Backend is one media centre instance. It is split by concern so a consumer can depend on only the part it uses.
type Backend interface {
	Source
	Player
	Remote
	Host
}

// Source reads the library and reports changes to it.
type Source interface {
	Info(ctx context.Context) (Info, error)

	// Library returns every item of kind. Episodes are not listed this way; use Episodes.
	Library(ctx context.Context, kind Kind) ([]Item, error)
	// Episodes returns a show's episodes in season and episode order.
	Episodes(ctx context.Context, showID int) ([]Item, error)
	// RecentlyAdded returns items of kind added after since, newest first.
	RecentlyAdded(ctx context.Context, kind Kind, since time.Time) ([]Item, error)
	// Item fetches one item.
	Item(ctx context.Context, ref Ref) (Item, error)
	// NextEpisode returns the first unwatched episode of a show, or ErrNotFound when it's all watched.
	NextEpisode(ctx context.Context, showID int) (Item, error)

	// Subscribe calls fn for each event until ctx is done, reconnecting as needed. fn must not block.
	Subscribe(ctx context.Context, fn func(Event)) error
}

// Player starts and controls playback.
type Player interface {
	Play(ctx context.Context, ref Ref, opts PlayOptions) error
	// Queue replaces the play queue with refs, in order, and starts the first. The refs must be all songs or all video.
	Queue(ctx context.Context, refs []Ref) error
	// PlayFile plays a path or URL outside the library, such as a trailer.
	PlayFile(ctx context.Context, path string) error
	// PartyMode plays random music until stopped.
	PartyMode(ctx context.Context) error

	Transport(ctx context.Context, cmd Transport) error
	Seek(ctx context.Context, s Seek) error
	Volume(ctx context.Context, change VolumeChange) (VolumeState, error)
	// NowPlaying returns nil when nothing is playing.
	NowPlaying(ctx context.Context) (*NowPlaying, error)

	// SetShuffle turns shuffle on or off for the active player.
	SetShuffle(ctx context.Context, on bool) error
	// SetRepeat sets the active player's repeat mode and returns the mode now in effect; RepeatCycle steps off, one, all.
	SetRepeat(ctx context.Context, mode Repeat) (Repeat, error)
	// Picture zooms, rotates or pans the playing video.
	Picture(ctx context.Context, cmd PictureCommand) error

	// Streams lists the playing item's audio and subtitle streams.
	Streams(ctx context.Context) (Streams, error)
	SetSubtitle(ctx context.Context, c StreamChange) error
	SetAudioStream(ctx context.Context, c StreamChange) error
}

// Remote drives the media centre's user interface like a remote control.
type Remote interface {
	Input(ctx context.Context, action InputAction) error
	// SendText types text into the focused input field.
	SendText(ctx context.Context, text string) error
	// ShowView opens a library view or dialog.
	ShowView(ctx context.Context, v View) error

	Favourites(ctx context.Context) ([]Favourite, error)
	OpenFavourite(ctx context.Context, f Favourite) error

	// Addons lists the installed, enabled add-ons that can be launched.
	Addons(ctx context.Context) ([]Addon, error)
	// ExecuteAddon launches an add-on with optional parameters.
	ExecuteAddon(ctx context.Context, id string, params map[string]string) error
}

// Host controls the machine and the library databases.
type Host interface {
	// Clock returns the wall-clock time where the media centre is. Only the clock reading is meaningful; the location may be UTC.
	Clock(ctx context.Context) (time.Time, error)
	System(ctx context.Context, cmd SystemCommand) error
	// ScanLibrary looks for new content; CleanLibrary removes entries whose files are gone.
	ScanLibrary(ctx context.Context, db Database) error
	CleanLibrary(ctx context.Context, db Database) error
}

// Repeat is a player's repeat mode.
type Repeat string

// PictureCommand adjusts how the playing video is shown.
type PictureCommand string

// Stream is one audio or subtitle stream of the playing item.
type Stream struct {
	Index int
	Name  string
	// Language is the stream's language as the backend reports it: a BCP 47 tag ("en", "pt-BR") or an ISO 639-2 code ("eng", "fre"). Empty when unknown.
	Language string
}

// Streams are the playing item's audio and subtitle streams.
type Streams struct {
	Audio        []Stream
	CurrentAudio *Stream
	Subtitles    []Stream
	// CurrentSubtitle is the selected subtitle, shown only when SubtitlesOn.
	CurrentSubtitle *Stream
	SubtitlesOn     bool
}

// StreamStep moves between streams or turns subtitles on and off.
type StreamStep string

// StreamChange picks a stream: Step when set, otherwise the stream at Index. Picking a subtitle by index also turns subtitles on.
type StreamChange struct {
	Step  StreamStep
	Index int
}

// InputAction is a remote control button or a user interface toggle.
type InputAction string

// Section is a library view or dialog.
type Section string

// View is a view to open.
type View struct {
	Section Section
	// Genre narrows the movies, TV shows, music videos or music sections to one genre, named as the library names it.
	Genre string
	// Playlist is the playlist for SectionPlaylist.
	Playlist Ref
}

// Favourite is an entry in the media centre's favourites.
type Favourite struct {
	Title string
	// Type is "media", "window", "script" and so on, as the backend reports it.
	Type            string
	Path            string
	Window          string
	WindowParameter string
}

// Addon is an installed add-on.
type Addon struct {
	ID   string
	Name string
	// Type is the add-on's kind, e.g. "xbmc.python.pluginsource".
	Type string
}

// SystemCommand changes the media centre's power state or ejects a disc.
type SystemCommand string

// Database is one of the media centre's library databases.
type Database string

// Call is one request a backend made to its media centre, e.g. Kodi's "Player.Open".
type Call struct {
	Method   string
	Duration time.Duration
	Err      error
}

type callObserverKey struct{}
