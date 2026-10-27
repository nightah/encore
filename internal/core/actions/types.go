package actions

import (
	"context"
	"log/slog"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/nightah/encore/internal/core/match"
	"github.com/nightah/encore/internal/core/media"
)

// Matcher finds library titles; *match.Index implements it.
type Matcher interface {
	Search(ctx context.Context, q match.Query) (match.Result, error)
}

// Library is the instance's in-memory catalog; *library.Catalog implements it.
type Library interface {
	// Lookup is the live re-query fallback: it asks the backend directly for titles the index doesn't know yet, adds them, and returns them.
	Lookup(ctx context.Context, kind media.Kind, title string) ([]media.Item, error)
	// Items returns every cached item of kind. Discovery actions (random picks, genres, what's new) read it instead of asking the backend.
	Items(kind media.Kind) []media.Item
}

// Target is the media centre a request controls.
type Target struct {
	// Name is the user-facing name, e.g. "Lounge".
	Name    string
	Backend media.Backend
	Matcher Matcher
	// Library is optional; without it a miss is final.
	Library Library
	// Wake is optional; when set, an unreachable backend is woken and the action retried once.
	Wake func(ctx context.Context) error
	// Language is the library's language, used to compare spoken names with titles; empty means English.
	Language string
	// QueueLimit caps how many items a shuffle or queue plays (Kanzi's playlist_max_items); zero means DefaultQueueLimit.
	QueueLimit int
	// ShutdownQuits makes "shut down" quit Kodi instead of powering off the machine.
	ShutdownQuits bool
	// DeepSearch lets a request that doesn't say what kind of title it wants ("play X") match music as well as films and TV.
	DeepSearch bool
	// ConfirmSound acknowledges a success with nothing to say with a short sound instead of silence.
	ConfirmSound bool
	// Disabled lists the content groups the skill was set up without; the zero value allows everything.
	Disabled []media.Content
}

// Message is a localisable message: an ID from the response catalogue and its placeholder values. An empty ID means say nothing.
type Message struct {
	ID     string
	Params map[string]string
	// Durations are placeholder values the renderer words for the listener's language, e.g. "1 hour 12 minutes".
	Durations map[string]time.Duration
	// Times are clock times the renderer words for the listener, e.g. "9:42 pm".
	Times map[string]time.Time
	// Languages are language codes (BCP 47 or ISO 639-2) the renderer names in the listener's language, e.g. "fre" becomes "French".
	Languages map[string]string
	// Lists are joined the listener's way, e.g. "Dune, Alien and Heat".
	Lists map[string][]string
}

// Choice is one option offered in a "did you mean" question. It round-trips through assistant session state as JSON.
type Choice struct {
	DocID string    `json:"doc_id"`
	Ref   media.Ref `json:"ref"`
	Title string    `json:"title"`
	Year  int       `json:"year,omitempty"`
}

// Outcome is the result of an action.
type Outcome struct {
	Say      Message
	Reprompt *Message
	// EndSession is false when the action expects a reply, e.g. after asking which title was meant.
	EndSession bool
	// Pending lists the choices offered by a question, for the adapter to keep in session state.
	Pending []Choice
	// Followup is what a question's answer should do, for the adapter to keep in session state alongside Pending.
	Followup *Followup
	// Confirm marks a success with nothing to say, which the adapter may acknowledge with a sound.
	Confirm bool
	// Played is the item that started playing, if any.
	Played *media.Item
	// Match is the search result behind the outcome, for the request log.
	Match *match.Result
	// Err is the internal error behind a failure message, for logging only.
	Err error
}

// Progress reports interim status during slow actions, e.g. "Waking the bedroom TV…". It may be nil.
type Progress func(ctx context.Context, msg Message)

// Followup records what a question is waiting for. It round-trips through assistant session state as JSON.
type Followup struct {
	// Recommend is the recommendation being offered; "no" asks for another like it.
	Recommend *RecommendRequest `json:"recommend,omitempty"`
	// Confirm is a system command waiting for "yes".
	Confirm media.SystemCommand `json:"confirm,omitempty"`
}

// Service runs actions.
type Service struct {
	log *slog.Logger
	now func() time.Time

	// Requests run concurrently and a rand.Rand is not safe for that.
	randMu sync.Mutex
	rand   *rand.Rand
}

// RecommendRequest asks for something to watch or listen to. It round-trips through session state so "no" can ask for another.
type RecommendRequest struct {
	// Kinds are the kinds to choose from, e.g. movies only, or anything to listen to.
	Kinds []media.Kind `json:"kinds"`
	// Genre narrows the choice, as spoken.
	Genre string `json:"genre,omitempty"`
}

// WhatsNewRequest asks what's been added: new movies (optionally one genre), new episodes (optionally of one show), new albums or new music videos.
type WhatsNewRequest struct {
	Kind  media.Kind
	Genre string
	Show  string
}

// MusicRequest names music to play. Which fields apply depends on the action.
type MusicRequest struct {
	Title  string
	Artist string
	Album  string
	Genre  string
	// Shuffle plays a container in random order.
	Shuffle bool
}

// TitleRequest asks to play something by name.
type TitleRequest struct {
	// Query is what was heard.
	Query string
	// Kinds restricts the search; empty searches every kind ("play X").
	Kinds []media.Kind
	// Hints boosts specific documents, e.g. Alexa's own entity resolution.
	Hints map[string]float64
	// Pending are the choices from a question this request answers.
	Pending []Choice
}

// SeekRequest moves the playhead. By wins over Step; To is an absolute position.
type SeekRequest struct {
	// By is a relative jump; negative goes back.
	By time.Duration
	// Step is a small or big step when no duration was given.
	Step media.SeekStep
	// To jumps to a position, e.g. "go to minute 42".
	To *time.Duration
}

// StreamRequest changes the audio or subtitle stream: Step when set, otherwise the stream in Language.
type StreamRequest struct {
	Step media.StreamStep
	// Language is a spoken language name, e.g. "French" or "Français".
	Language string
	// Listener is the listener's language (BCP 47), used to understand Language.
	Listener string
}
