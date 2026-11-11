package media

import (
	"strconv"
	"time"
)

// Contents lists every content group.
var Contents = []Content{ContentMovies, ContentTV, ContentMusic, ContentLiveTV}

// LibraryContents lists the content groups backed by the library, leaving out live TV.
var LibraryContents = []Content{ContentMovies, ContentTV, ContentMusic}

// Qualities lists the video qualities, lowest first.
var Qualities = []Quality{QualitySD, Quality720p, Quality1080p, Quality2160p}

// ContentOf returns the group a kind belongs to, or "" for kinds outside any group, such as playlists.
func ContentOf(k Kind) Content {
	switch k {
	case KindMovie:
		return ContentMovies
	case KindTVShow, KindEpisode:
		return ContentTV
	case KindArtist, KindAlbum, KindSong, KindMusicVideo:
		return ContentMusic
	case KindChannel:
		return ContentLiveTV
	}

	return ""
}

// Cover is the image to show for the item: the poster, else the thumbnail (an episode's still or an album cover).
func (a Artwork) Cover() string {
	if a.Poster != "" {
		return a.Poster
	}

	return a.Thumb
}

// Ref returns the item's reference.
func (i Item) Ref() Ref { return Ref{Kind: i.Kind, ID: i.ID} }

// InProgress reports whether playback was stopped part way through.
func (i Item) InProgress() bool { return i.Resume > 0 }

// Remaining is how long is left of the current item.
func (n NowPlaying) Remaining() time.Duration { return max(0, n.Duration-n.Position) }

// Number is the channel's number as it is shown, such as "24" or "7.2".
func (c Channel) Number() string {
	if c.SubNumber > 0 {
		return strconv.Itoa(c.Major) + "." + strconv.Itoa(c.SubNumber)
	}

	return strconv.Itoa(c.Major)
}

// OnAt reports whether the broadcast is airing at t.
func (b Broadcast) OnAt(t time.Time) bool { return !t.Before(b.Start) && t.Before(b.End) }

// IsValid reports whether m is a search mode, or empty for the default.
func (m SearchMode) IsValid() bool {
	return m == "" || m == SearchModeLibrary || m == SearchModeGlobal || m == SearchModeSkin
}
