package media

// Library item kinds.
const (
	KindMovie      Kind = "movie"
	KindTVShow     Kind = "tvshow"
	KindEpisode    Kind = "episode"
	KindArtist     Kind = "artist"
	KindAlbum      Kind = "album"
	KindSong       Kind = "song"
	KindMusicVideo Kind = "musicvideo"
	KindPlaylist   Kind = "playlist"
)

// Content groups, in the order they are offered.
const (
	ContentMovies Content = "movies"
	ContentTV     Content = "tv"
	ContentMusic  Content = "music"
)

// Transport commands.
const (
	TransportPause     Transport = "pause"
	TransportResume    Transport = "resume"
	TransportStop      Transport = "stop"
	TransportNext      Transport = "next"
	TransportPrevious  Transport = "previous"
	TransportStartOver Transport = "startover"
)

// Seek steps.
const (
	SeekSmallForward  SeekStep = "smallforward"
	SeekSmallBackward SeekStep = "smallbackward"
	SeekBigForward    SeekStep = "bigforward"
	SeekBigBackward   SeekStep = "bigbackward"
)

// Event kinds.
const (
	EventItemUpdated     EventKind = "item_updated"
	EventItemRemoved     EventKind = "item_removed"
	EventLibraryScanned  EventKind = "library_scanned"
	EventPlaybackStarted EventKind = "playback_started"
	EventPlaybackStopped EventKind = "playback_stopped"
	EventConnected       EventKind = "connected"
	EventDisconnected    EventKind = "disconnected"
)

// Repeat modes. RepeatCycle is a request only: step to the next mode.
const (
	RepeatOff   Repeat = "off"
	RepeatOne   Repeat = "one"
	RepeatAll   Repeat = "all"
	RepeatCycle Repeat = "cycle"
)

// Picture commands.
const (
	PictureZoomIn                 PictureCommand = "zoomin"
	PictureZoomOut                PictureCommand = "zoomout"
	PictureZoomReset              PictureCommand = "zoomreset"
	PictureRotateClockwise        PictureCommand = "rotateclockwise"
	PictureRotateCounterClockwise PictureCommand = "rotatecounterclockwise"
	PictureMoveUp                 PictureCommand = "moveup"
	PictureMoveDown               PictureCommand = "movedown"
	PictureMoveLeft               PictureCommand = "moveleft"
	PictureMoveRight              PictureCommand = "moveright"
)

// Stream steps. StreamOn and StreamOff apply to subtitles only.
const (
	StreamNext     StreamStep = "next"
	StreamPrevious StreamStep = "previous"
	StreamOn       StreamStep = "on"
	StreamOff      StreamStep = "off"
)

// Input actions.
const (
	InputUp           InputAction = "up"
	InputDown         InputAction = "down"
	InputLeft         InputAction = "left"
	InputRight        InputAction = "right"
	InputSelect       InputAction = "select"
	InputBack         InputAction = "back"
	InputHome         InputAction = "home"
	InputInfo         InputAction = "info"
	InputContextMenu  InputAction = "contextmenu"
	InputPageUp       InputAction = "pageup"
	InputPageDown     InputAction = "pagedown"
	InputOSD          InputAction = "osd"
	InputFullscreen   InputAction = "fullscreen"
	InputStereoscopic InputAction = "stereoscopic"
	InputPassthrough  InputAction = "passthrough"
)

// Sections.
const (
	SectionMovies            Section = "movies"
	SectionTVShows           Section = "tvshows"
	SectionMusicVideos       Section = "musicvideos"
	SectionMusic             Section = "music"
	SectionArtists           Section = "artists"
	SectionAlbums            Section = "albums"
	SectionRecentMovies      Section = "recentmovies"
	SectionRecentEpisodes    Section = "recentepisodes"
	SectionRecentMusicVideos Section = "recentmusicvideos"
	SectionRecentMusic       Section = "recentmusic"
	SectionFavourites        Section = "favourites"
	SectionPlaylist          Section = "playlist"
	SectionSubtitleSearch    Section = "subtitlesearch"
)

// System commands. SystemQuit exits the media centre application without touching the machine.
const (
	SystemShutdown  SystemCommand = "shutdown"
	SystemReboot    SystemCommand = "reboot"
	SystemHibernate SystemCommand = "hibernate"
	SystemSuspend   SystemCommand = "suspend"
	SystemQuit      SystemCommand = "quit"
	SystemEject     SystemCommand = "eject"
)

// Databases.
const (
	DatabaseVideo Database = "video"
	DatabaseMusic Database = "music"
)
