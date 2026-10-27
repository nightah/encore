package media

import (
	"time"
)

// Contents lists every content group.
var Contents = []Content{ContentMovies, ContentTV, ContentMusic}

// ContentOf returns the group a kind belongs to, or "" for kinds outside any group, such as playlists.
func ContentOf(k Kind) Content {
	switch k {
	case KindMovie:
		return ContentMovies
	case KindTVShow, KindEpisode:
		return ContentTV
	case KindArtist, KindAlbum, KindSong, KindMusicVideo:
		return ContentMusic
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
