package deploy

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/nightah/encore/internal/skillmodel"
	"github.com/nightah/encore/internal/smapi"
)

// OpenPhrase is the locale's open phrase from src with invocation in place, or "" when src has none for locale.
func OpenPhrase(src *skillmodel.Sources, locale, invocation string) string {
	open := src.Locales[locale].Launch.Open
	if open == "" {
		return ""
	}

	return skillmodel.Say(open, invocation)
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

	phrase := OpenPhrase(r.d.src, loc, r.pkg.Invocation(loc))
	r.start(StepSimulate, fmt.Sprintf("Saying “%s” to the simulator in %s", phrase, loc))

	sctx, cancel := context.WithTimeout(ctx, simulateTimeout)
	sim, err := r.simulator().untilResolved(sctx, loc, phrase)
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
		return slices.Contains(r.pkg.Locales(), loc) && smapi.CanSimulate(loc) && OpenPhrase(r.d.src, loc, r.pkg.Invocation(loc)) != ""
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

func (r *run) simulator() simulator {
	return simulator{client: r.d.client, skillID: r.res.State.SkillID, debug: r.debug}
}

// untilResolved asks again, at the poll interval and up to simulateAttempts times, while the simulator says phrase resolves to no intent.
func (s simulator) untilResolved(ctx context.Context, locale, phrase string) (*smapi.Simulation, error) {
	var (
		sim      *smapi.Simulation
		attempts int
	)

	err := s.client.Poll(ctx, func(ctx context.Context) (bool, error) {
		attempts++

		var err error
		if sim, err = s.once(ctx, locale, phrase); err != nil {
			return false, err
		}

		unresolved := sim.Status == smapi.SimulationFailed && strings.Contains(sim.Error, "did not resolve to any intent")
		if unresolved && attempts < simulateAttempts {
			s.debug("skill simulation not resolved yet; asking again", "locale", locale, "attempt", attempts)

			return false, nil
		}

		return true, nil
	})

	return sim, err
}

// once says phrase and waits for the simulator's answer.
func (s simulator) once(ctx context.Context, locale, phrase string) (*smapi.Simulation, error) {
	id, err := s.client.Simulate(ctx, s.skillID, locale, phrase)
	if err != nil {
		return nil, err
	}

	var sim *smapi.Simulation

	err = s.client.Poll(ctx, func(ctx context.Context) (bool, error) {
		sim, err = s.client.Simulation(ctx, s.skillID, id)
		if err != nil {
			return false, err
		}

		return sim.Status != smapi.SimulationInProgress, nil
	})

	return sim, err
}
