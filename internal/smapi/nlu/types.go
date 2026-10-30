package nlu

import (
	"context"
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
	// Parallel is how many utterances are profiled at once. Default 4.
	Parallel int
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
}

// Outcome is how a row did, given whether it was pending.
type Outcome string

// Summary counts results by outcome.
type Summary struct {
	Pass, Fail, Pending, Fixed int
}
