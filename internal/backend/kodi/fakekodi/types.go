package fakekodi

import (
	"encoding/json"
	"net/http/httptest"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// Options configure a Server.
type Options struct {
	// Version is the JSON-RPC API version reported by JSONRPC.Version. Defaults to 13.5.0 (Kodi 21 Omega).
	Version [3]int
	// Username and Password enable HTTP basic auth on HTTP POST requests, as Kodi does. WebSocket connections are never authenticated, also as Kodi.
	Username, Password string
	// Library seeds the media library; defaults to DefaultLibrary(). Change it while the server runs with Update. Several servers may share one Library, as Kodi instances share a MySQL database: changes made through any of them are visible to all, while notifications come only from the server that made the change.
	Library *Library
	// MAC is reported as Network.MacAddress; defaults to DC:A6:32:4B:19:0E.
	MAC string
	// DatabaseName is what Database.GetDatabaseName reports for the video database, e.g. "MyVideos131". Empty leaves the method out, as before Kodi 22.
	DatabaseName string
}

// Server is a fake Kodi.
type Server struct {
	opts Options

	mu       sync.Mutex
	handlers map[string]HandlerFunc
	calls    []Call
	clients  map[*websocket.Conn]struct{}
	// accepting counts WebSocket handshakes under way; settled is signalled as each one finishes.
	accepting int
	settled   *sync.Cond
	state     playerState
	mac       string
	wsOff     bool
	addr      string

	// afterAccept, when set before the server starts, runs between a WebSocket handshake and the client being registered, so tests can widen that gap.
	afterAccept func()

	httpServer *httptest.Server
}

// HandlerFunc answers one method call. params is the raw params member, which may be absent.
type HandlerFunc func(params json.RawMessage) (any, *Error)

// Call is one recorded request.
type Call struct {
	Method string
	Params json.RawMessage
	// WebSocket is true for calls received over the WebSocket, false for HTTP POST.
	WebSocket bool
}

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
}

// Library is the fake's media library. JSON shapes follow what Kodi returns when every property Encore asks for is requested.
type Library struct {
	Movies      []Movie
	TVShows     []TVShow
	Episodes    []Episode
	MusicVideos []MusicVideo
	Artists     []Artist
	Albums      []Album
	Songs       []Song
	// Playlists are the files under special://profile/playlists/{video,music}.
	Playlists []Playlist
	// Favourites and Addons back Favourites.GetFavourites and Addons.GetAddons.
	Favourites []Favourite
	Addons     []Addon

	// Guards the library across the servers sharing it.
	mu sync.Mutex
}

type record interface {
	id() int
	title() string
	playcount() int
	dateadded() string
}

// Movie is a library movie.
type Movie struct {
	MovieID       int      `json:"movieid"`
	Label         string   `json:"label"`
	Title         string   `json:"title"`
	SortTitle     string   `json:"sorttitle"`
	OriginalTitle string   `json:"originaltitle"`
	Year          int      `json:"year"`
	PlayCount     int      `json:"playcount"`
	Runtime       int      `json:"runtime"`
	DateAdded     string   `json:"dateadded"`
	LastPlayed    string   `json:"lastplayed"`
	Genre         []string `json:"genre"`
	Resume        Resume   `json:"resume"`
	File          string   `json:"file,omitempty"`
	Trailer       string   `json:"trailer"`
	Set           string   `json:"set"`
	SetID         int      `json:"setid"`
}

// TVShow is a library TV show.
type TVShow struct {
	TVShowID        int      `json:"tvshowid"`
	Label           string   `json:"label"`
	Title           string   `json:"title"`
	SortTitle       string   `json:"sorttitle"`
	OriginalTitle   string   `json:"originaltitle"`
	Year            int      `json:"year"`
	Genre           []string `json:"genre"`
	PlayCount       int      `json:"playcount"`
	Episode         int      `json:"episode"`
	WatchedEpisodes int      `json:"watchedepisodes"`
	DateAdded       string   `json:"dateadded"`
	LastPlayed      string   `json:"lastplayed"`
}

// Episode is a library episode.
type Episode struct {
	EpisodeID     int    `json:"episodeid"`
	TVShowID      int    `json:"tvshowid"`
	Label         string `json:"label"`
	Title         string `json:"title"`
	OriginalTitle string `json:"originaltitle"`
	ShowTitle     string `json:"showtitle"`
	Season        int    `json:"season"`
	Episode       int    `json:"episode"`
	PlayCount     int    `json:"playcount"`
	Runtime       int    `json:"runtime"`
	DateAdded     string `json:"dateadded"`
	LastPlayed    string `json:"lastplayed"`
	Resume        Resume `json:"resume"`
	File          string `json:"file,omitempty"`
}

// MusicVideo is a library music video.
type MusicVideo struct {
	MusicVideoID int      `json:"musicvideoid"`
	Label        string   `json:"label"`
	Title        string   `json:"title"`
	Artist       []string `json:"artist"`
	Album        string   `json:"album"`
	Year         int      `json:"year"`
	Genre        []string `json:"genre"`
	PlayCount    int      `json:"playcount"`
	Runtime      int      `json:"runtime"`
	DateAdded    string   `json:"dateadded"`
	LastPlayed   string   `json:"lastplayed"`
	Resume       Resume   `json:"resume"`
}

// Artist is a library artist.
type Artist struct {
	ArtistID  int      `json:"artistid"`
	Label     string   `json:"label"`
	Artist    string   `json:"artist"`
	SortName  string   `json:"sortname"`
	Genre     []string `json:"genre"`
	DateAdded string   `json:"dateadded"`
}

// Album is a library album.
type Album struct {
	AlbumID       int      `json:"albumid"`
	Label         string   `json:"label"`
	Title         string   `json:"title"`
	Artist        []string `json:"artist"`
	DisplayArtist string   `json:"displayartist"`
	Year          int      `json:"year"`
	Genre         []string `json:"genre"`
	PlayCount     int      `json:"playcount"`
	DateAdded     string   `json:"dateadded"`
	LastPlayed    string   `json:"lastplayed"`
}

// Song is a library song.
type Song struct {
	SongID        int      `json:"songid"`
	Label         string   `json:"label"`
	Title         string   `json:"title"`
	Artist        []string `json:"artist"`
	DisplayArtist string   `json:"displayartist"`
	Album         string   `json:"album"`
	AlbumID       int      `json:"albumid"`
	Year          int      `json:"year"`
	Genre         []string `json:"genre"`
	Track         int      `json:"track"`
	Duration      int      `json:"duration"`
	PlayCount     int      `json:"playcount"`
	DateAdded     string   `json:"dateadded"`
	LastPlayed    string   `json:"lastplayed"`
}

// Resume is a saved playback position in seconds. Kodi always reports it, zero when there is none.
type Resume struct {
	Position float64 `json:"position"`
	Total    float64 `json:"total"`
}

// Playlist is a playlist file as Files.GetDirectory lists it.
type Playlist struct {
	File  string `json:"file"`
	Label string `json:"label"`
	// Media is "video" or "music", the playlists folder it lives in.
	Media string `json:"-"`
	// Entries are the files the playlist expands to.
	Entries []string `json:"-"`
}

// Favourite is a Favourites.GetFavourites entry.
type Favourite struct {
	Title           string `json:"title"`
	Type            string `json:"type"`
	Path            string `json:"path,omitempty"`
	Window          string `json:"window,omitempty"`
	WindowParameter string `json:"windowparameter,omitempty"`
}

// Addon is an Addons.GetAddons entry.
type Addon struct {
	AddonID string `json:"addonid"`
	Type    string `json:"type"`
	Name    string `json:"name"`
	Enabled bool   `json:"-"`
}

// PlayState is what Kodi saves for a movie or episode when playback stops.
type PlayState struct {
	PlayCount int
	// Resume is the saved position in seconds; zero clears it.
	Resume float64
	// LastPlayed is formatted in the local time zone, as Kodi does; zero leaves it unchanged.
	LastPlayed time.Time
}

type playerState struct {
	volume int
	muted  bool

	playing  *Item
	paused   bool
	position float64
	queue    []Item
	index    int
	shuffled bool

	repeat      string
	subtitlesOn bool
	audio       int
	subtitle    int
	clock       string

	playlists map[int][]Item
}

// Item is what Player.GetItem reports.
type Item struct {
	Type      string   `json:"type"`
	ID        int      `json:"id,omitempty"`
	Label     string   `json:"label"`
	Title     string   `json:"title,omitempty"`
	ShowTitle string   `json:"showtitle,omitempty"`
	TVShowID  int      `json:"tvshowid,omitempty"`
	Season    int      `json:"season,omitempty"`
	Episode   int      `json:"episode,omitempty"`
	Artist    []string `json:"artist,omitempty"`
	Album     string   `json:"album,omitempty"`
	Year      int      `json:"year,omitempty"`
	Genre     []string `json:"genre,omitempty"`
	Runtime   int      `json:"runtime,omitempty"`
	Duration  int      `json:"duration,omitempty"`
	File      string   `json:"file,omitempty"`
	// Art references images the fake's web server serves at /image/.
	Art map[string]string `json:"art,omitempty"`

	resume float64
}

// A Player.Open or Playlist.Add item.
type itemRef struct {
	MovieID      int    `json:"movieid"`
	EpisodeID    int    `json:"episodeid"`
	MusicVideoID int    `json:"musicvideoid"`
	SongID       int    `json:"songid"`
	AlbumID      int    `json:"albumid"`
	ArtistID     int    `json:"artistid"`
	File         string `json:"file"`
	PlaylistID   *int   `json:"playlistid"`
	Position     int    `json:"position"`
	PartyMode    string `json:"partymode"`
}

// Stream is an audio or subtitle stream as Player.GetProperties reports it.
type Stream struct {
	Index    int    `json:"index"`
	Name     string `json:"name"`
	Language string `json:"language"`
}

// Only the subset of Kodi's list parameters the fake honours.
type listParams struct {
	TVShowID int         `json:"tvshowid"`
	Season   *int        `json:"season"`
	AlbumID  int         `json:"albumid"`
	Filter   *filter     `json:"filter"`
	Limits   *listLimits `json:"limits"`
	Sort     *listSort   `json:"sort"`
}

type listSort struct {
	Method string `json:"method"`
	Order  string `json:"order"`
}

type listLimits struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// A Kodi list filter rule, or an "and"/"or" of them.
type filter struct {
	Field    string    `json:"field"`
	Operator string    `json:"operator"`
	Value    any       `json:"value"`
	And      []*filter `json:"and"`
	Or       []*filter `json:"or"`
	// AlbumID and ArtistID are AudioLibrary.GetSongs' ID filters.
	AlbumID  *int `json:"albumid"`
	ArtistID *int `json:"artistid"`
}
