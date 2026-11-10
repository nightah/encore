package actions

import "time"

// DefaultQueueLimit is the queue cap when a target doesn't set one, as in Kanzi.
const DefaultQueueLimit = 100

const volumeStep = 10

const whatsNewMax = 5

// newEpisodesWindow is how far back "are there new shows" looks for unwatched episodes.
const newEpisodesWindow = 30 * 24 * time.Hour

// candidateLimit is how many matches a filtered lookup ("X by Y") considers.
const candidateLimit = 20

// pendingBoost is added to titles offered in the previous question, so a request that repeats one of their names resolves to it.
const pendingBoost = 0.08

// maxChoices is how many titles one "did you mean" question offers.
const maxChoices = 2

// maxCandidates is how many of a search's titles a "did you mean" question keeps, counting those it offers; "no" offers the rest, maxChoices at a time.
const maxCandidates = 6

// nearlyOver is when "how long is left" just says it's nearly finished.
const nearlyOver = time.Minute

// maxPresses caps "down five times" so a misheard number can't scroll forever.
const maxPresses = 10

// globalSearchAddon is the add-on Kanzi's global search drove.
const globalSearchAddon = "script.globalsearch"

// lastShows is how many recently watched shows "continue watching" tries before giving up.
const lastShows = 5

// nukta is the Devanagari sign that marks a borrowed sound (फ़ f, ज़ z), which streamKey drops.
const nukta = "\u093c"

// Positions an Answer can name among the titles a question offered, and the kinds it can name ("the film", "the show").
const (
	AnswerFirst  = "first"
	AnswerSecond = "second"
	AnswerLast   = "last"
	AnswerNewer  = "newer"
	AnswerOlder  = "older"
	AnswerFilm   = "film"
	AnswerShow   = "show"
)

// scoreTie is how close two match scores are to count as a tie.
const scoreTie = 1e-6

// minYear and maxYear bound a number read as a release year in an answer.
const (
	minYear = 1880
	maxYear = 2100
)
