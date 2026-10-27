package deploy

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/nightah/encore/internal/smapi"
)

// openPhrases are per language; %s is the invocation name.
var openPhrases = map[string]string{
	"de": "öffne %s",
	"en": "open %s",
	"fr": "ouvre %s",
	"ja": "%sを開いて",
}

// OpenPhrase is the utterance that opens the skill in locale, or "" if Encore does not know one.
func OpenPhrase(locale, invocation string) string {
	lang, _, _ := strings.Cut(locale, "-")

	p, ok := openPhrases[lang]
	if !ok {
		return ""
	}

	return fmt.Sprintf(p, invocation)
}

// simulate opens the skill in one locale (see smokeLocale): that proves Alexa reaches the endpoint, while simulating every locale in turn took minutes. A failed smoke test is a warning, not a failed deploy: the skill is already live for testing.
func (r *run) simulate(ctx context.Context) error {
	if !r.req.Simulate {
		r.skip(StepSimulate, "Not requested")

		return nil
	}

	loc := r.smokeLocale()
	if loc == "" {
		r.skip(StepSimulate, "None of the locales can be simulated")

		return nil
	}

	phrase := OpenPhrase(loc, r.pkg.Invocation(loc))
	r.start(StepSimulate, fmt.Sprintf("Saying “%s” to the simulator in %s", phrase, loc))

	sctx, cancel := context.WithTimeout(ctx, simulateTimeout)
	sim, err := r.simulateUntilResolved(sctx, loc, phrase)
	cancel()

	if ctx.Err() != nil {
		return ctx.Err()
	}

	if err != nil {
		sim = &smapi.Simulation{Status: smapi.SimulationFailed, Error: err.Error()}
	}

	ls := r.res.Locales[loc]
	ls.Simulation = sim
	r.res.Locales[loc] = ls

	switch {
	case sim.Status != smapi.SimulationSuccessful:
		r.res.Warnings = append(r.res.Warnings, fmt.Sprintf("%s: simulating “%s” failed: %s", loc, phrase, sim.Error))
	case !sim.Invoked:
		r.res.Warnings = append(r.res.Warnings, fmt.Sprintf("%s: “%s” did not reach the skill; Alexa said “%s”", loc, phrase, strings.Join(sim.Captions, " ")))
	}

	r.debug("skill simulation", "locale", loc, "phrase", phrase, "status", sim.Status, "invoked", sim.Invoked, "error", sim.Error)
	r.emit(Event{Step: StepSimulate, State: StateDone, Locales: r.snapshot()})
	r.step = ""

	return nil
}

// smokeLocale is the locale to simulate: the primary locale when the package has it and the simulation API supports it, else the first package locale it supports, or "" when none.
func (r *run) smokeLocale() string {
	can := func(loc string) bool {
		return slices.Contains(r.pkg.Locales(), loc) && smapi.CanSimulate(loc) && OpenPhrase(loc, r.pkg.Invocation(loc)) != ""
	}

	if can(r.req.PrimaryLocale) {
		return r.req.PrimaryLocale
	}

	for _, loc := range r.pkg.Locales() {
		if can(loc) {
			return loc
		}
	}

	return ""
}

// simulateUntilResolved asks again, at the poll interval and up to simulateAttempts times, while the simulator says phrase resolves to no intent.
func (r *run) simulateUntilResolved(ctx context.Context, locale, phrase string) (*smapi.Simulation, error) {
	var (
		sim      *smapi.Simulation
		attempts int
	)

	err := r.d.client.Poll(ctx, func(ctx context.Context) (bool, error) {
		attempts++

		var err error
		if sim, err = r.simulateLocale(ctx, locale, phrase); err != nil {
			return false, err
		}

		unresolved := sim.Status == smapi.SimulationFailed && strings.Contains(sim.Error, "did not resolve to any intent")
		if unresolved && attempts < simulateAttempts {
			r.debug("skill simulation not resolved yet; asking again", "locale", locale, "attempt", attempts)

			return false, nil
		}

		return true, nil
	})

	return sim, err
}

func (r *run) simulateLocale(ctx context.Context, locale, phrase string) (*smapi.Simulation, error) {
	id, err := r.d.client.Simulate(ctx, r.res.State.SkillID, locale, phrase)
	if err != nil {
		return nil, err
	}

	var sim *smapi.Simulation

	err = r.d.client.Poll(ctx, func(ctx context.Context) (bool, error) {
		sim, err = r.d.client.Simulation(ctx, r.res.State.SkillID, id)
		if err != nil {
			return false, err
		}

		return sim.Status != smapi.SimulationInProgress, nil
	})

	return sim, err
}
