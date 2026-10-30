package deploy

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/nightah/encore/internal/smapi"
)

// CheckOpen says each locale's launch phrases (Launch.Phrases) to the simulator in turn, stopping at the first that reaches the skill, whether or not smapi.CanSimulate lists the locale: Amazon's list of simulator locales is narrower than what it accepts, and some locales it accepts never route an open phrase. invocation is the invocation choice the skill was deployed with (see skillmodel.Locale.InvocationName). A locale the simulator refuses outright gets one try, with the refusal as its error. The deploy's smoke test opens one locale; this is for a developer checking them all.
func (d *Deployer) CheckOpen(ctx context.Context, skillID string, locales []string, invocation string, log *slog.Logger) ([]OpenCheck, error) {
	debug := func(string, ...any) {}
	if log != nil {
		debug = log.Debug
	}

	sim := simulator{client: d.client, skillID: skillID, debug: debug}
	out := make([]OpenCheck, 0, len(locales))

	for _, locale := range locales {
		loc := d.src.Locales[locale]
		check := OpenCheck{Locale: locale}

		for _, phrase := range loc.Launch.Phrases(loc.InvocationName(invocation)) {
			sctx, cancel := context.WithTimeout(ctx, simulateTimeout)
			s, err := sim.untilResolved(sctx, locale, phrase)
			cancel()

			if ctx.Err() != nil {
				return out, ctx.Err()
			}

			if err != nil {
				s = &smapi.Simulation{Status: smapi.SimulationFailed, Error: err.Error()}
			}

			refused := smapi.StatusCode(err) == http.StatusBadRequest
			check.Tries = append(check.Tries, OpenTry{Phrase: phrase, Simulation: s, Refused: refused})
			debug("open check", "locale", locale, "phrase", phrase, "status", s.Status, "invoked", s.Invoked, "error", s.Error)

			if s.Invoked || refused {
				break
			}
		}

		out = append(out, check)
	}

	return out, nil
}

// Reached reports whether any phrase reached the skill.
func (c OpenCheck) Reached() bool {
	for _, t := range c.Tries {
		if t.Simulation != nil && t.Simulation.Invoked {
			return true
		}
	}

	return false
}

// Refused reports whether the simulator rejected the locale rather than answering a phrase.
func (c OpenCheck) Refused() bool {
	for _, t := range c.Tries {
		if t.Refused {
			return true
		}
	}

	return false
}
