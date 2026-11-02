package nlu

import (
	"context"
	"sync"
	"time"

	"github.com/nightah/encore/internal/smapi"
)

// Fixture is one locale's file of utterances.
type Fixture struct {
	Locale string `yaml:"locale"`
	Cases  []Case `yaml:"cases"`
}

// Case is one utterance and what NLU should make of it.
type Case struct {
	Utterance string `yaml:"utterance"`
	Intent    string `yaml:"intent"`
	// Slots must be filled. An empty value accepts any words; otherwise the words, or a value or ID they resolve to, must equal it, ignoring case.
	Slots map[string]string `yaml:"slots"`
	// Pending says why the row is expected to fail until the next deploy.
	Pending string `yaml:"pending"`
	// Also are other readings that do what the row asks just as well, such as "repeat off" as Repeat with RepeatMode off rather than AMAZON.LoopOffIntent.
	Also []Reading `yaml:"also"`
}

// Reading is an intent and the slots it fills: a fixture row's alternative, or a reading Encore takes of a PlayMedia query (Runner.Reread).
type Reading struct {
	Intent string            `yaml:"intent"`
	Slots  map[string]string `yaml:"slots"`
}

// Profiler profiles an utterance; *smapi.Client is one.
type Profiler interface {
	ProfileNLU(ctx context.Context, skillID, locale, utterance string) (*smapi.NLUProfile, error)
}

// Runner profiles fixtures against one skill. Zero fields take the defaults.
type Runner struct {
	Client  Profiler
	SkillID string
	// Reprobes is how many more times an utterance that selects no intent is asked before that counts. Default 2.
	Reprobes int
	// Wait is the pause before each reprobe. Default 2s.
	Wait time.Duration
	// Reread, if set, reads a PlayMedia search query as the film, TV or music intents Encore's handler would try (alexa.Speech.Reinterpret), so a row that arrives as PlayMedia passes when Encore reads it as the row expects.
	Reread func(locale, query string) []Reading
	// Rate is how many profileNlu requests a second are sent, reprobes included, across every fixture. Default 2, under Amazon's limit for a developer account.
	Rate float64
}

// pacer spaces requests at least interval apart.
type pacer struct {
	mu       sync.Mutex
	next     time.Time
	interval time.Duration
}

// Result is one row's outcome.
type Result struct {
	Locale string
	Case   Case
	// Got is NLU's last answer, nil when profiling failed.
	Got *smapi.NLUProfile
	Err error
	// Problems say how Got differs from the row.
	Problems []string
	// Probes counts the profileNlu calls made for the row.
	Probes int
	// Via names how a passing row got there when not as its own intent: one of its alternatives, or a reading of PlayMedia's query.
	Via string
}

// Outcome is how a row did, given whether it was pending.
type Outcome string

// Summary counts results by outcome.
type Summary struct {
	Pass, Fail, Pending, Fixed int
}
