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
	// Via is set when the request named a room or device; a room without its own Kodi instance plays on the Echo's, so the target can be another room's.
	Via Via
}

// Via is the room name a request said, e.g. "TV", a device in the lounge.
type Via struct {
	// Spoken is the name that matched, Kind "room" or "device", and Room the name of the room it belongs to.
	Spoken, Kind, Room string
}

// Message is a localisable message: an ID from the response catalogue and its placeholder values. An empty ID means say nothing. It round-trips through assistant session state as JSON, so a question can be asked again.
type Message struct {
	ID     string            `json:"id"`
	Params map[string]string `json:"params,omitempty"`
	// Durations are placeholder values the renderer words for the listener's language, e.g. "1 hour 12 minutes".
	Durations map[string]time.Duration `json:"durations,omitempty"`
	// Times are clock times the renderer words for the listener, e.g. "9:42 pm".
	Times map[string]time.Time `json:"times,omitempty"`
	// Languages are language codes (BCP 47 or ISO 639-2) the renderer names in the listener's language, e.g. "fre" becomes "French".
	Languages map[string]string `json:"languages,omitempty"`
	// Lists are joined the listener's way, e.g. "Dune, Alien and Heat".
	Lists map[string][]string `json:"lists,omitempty"`
	// Messages are placeholder values that are messages themselves, rendered in the listener's language, e.g. a title with its year.
	Messages map[string]Message `json:"messages,omitempty"`
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
	// AskAgain means an answer chose none of a question's titles: the adapter asks the question again after Say, whose {question} placeholder it fills, keeping the question's choices.
	AskAgain bool
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
	// More are the titles a "did you mean" question found but hasn't offered yet; "no" offers them next.
	More []Choice `json:"more,omitempty"`
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
	// Resolved are the values Alexa's entity resolution matched the request to, scored as candidates (match.Query.Resolved).
	Resolved []match.Resolved
	// Pending are the choices from a question this request answers.
	Pending []Choice
}

// Answer chooses one of the titles a "did you mean" question offered.
type Answer struct {
	// Which is a position among the titles offered (AnswerFirst, AnswerSecond...); it wins over Title.
	Which string
	// Title is a title said on its own: one the question found, or another to search for.
	Title string
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
