package nlu

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/nightah/encore/internal/smapi"
)

// Run profiles every row of f and returns their results in row order. It returns early only when ctx ends.
func (r *Runner) Run(ctx context.Context, f Fixture) ([]Result, error) {
	parallel := r.Parallel
	if parallel <= 0 {
		parallel = defaultParallel
	}

	out := make([]Result, len(f.Cases))
	sem := make(chan struct{}, parallel)

	var wg sync.WaitGroup

	for i, c := range f.Cases {
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			wg.Wait()

			return nil, ctx.Err()
		}

		wg.Go(func() {
			defer func() { <-sem }()

			out[i] = r.one(ctx, f.Locale, c)
		})
	}

	wg.Wait()

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return out, nil
}

func (r *Runner) one(ctx context.Context, locale string, c Case) Result {
	reprobes := r.Reprobes
	if reprobes <= 0 {
		reprobes = defaultReprobes
	}

	wait := r.Wait
	if wait <= 0 {
		wait = defaultWait
	}

	res := Result{Locale: locale, Case: c}

	for {
		res.Probes++

		res.Got, res.Err = r.Client.ProfileNLU(ctx, r.SkillID, locale, c.Utterance)
		if res.Err != nil || res.Got.Intent() != "" || res.Probes > reprobes {
			break
		}

		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()

			res.Err = ctx.Err()

			return res
		case <-t.C:
		}
	}

	if res.Err == nil {
		res.Problems = compare(c, res.Got)
	}

	return res
}

// compare says how p differs from what c expects.
func compare(c Case, p *smapi.NLUProfile) []string {
	got := p.Intent()
	if got != c.Intent {
		if got == "" {
			got = "no intent"
		}

		return []string{fmt.Sprintf("got %s, want %s", got, c.Intent)}
	}

	var out []string

	for _, name := range sortedKeys(c.Slots) {
		want := c.Slots[name]
		slot, ok := p.Selected.Slots[name]

		switch {
		case !ok || strings.TrimSpace(slot.Value) == "":
			out = append(out, fmt.Sprintf("slot %s is empty", name))
		case want != "" && !matches(slot, want):
			out = append(out, fmt.Sprintf("slot %s is %q, want %q", name, slot.Value, want))
		}
	}

	return out
}

func matches(s smapi.NLUSlot, want string) bool {
	if strings.EqualFold(strings.TrimSpace(s.Value), want) {
		return true
	}

	for _, v := range s.Resolved() {
		if strings.EqualFold(v, want) {
			return true
		}
	}

	return false
}

// Outcome is how the row did.
func (r Result) Outcome() Outcome {
	passed := r.Err == nil && len(r.Problems) == 0

	switch {
	case passed && r.Case.Pending != "":
		return OutcomeFixed
	case passed:
		return OutcomePass
	case r.Case.Pending != "":
		return OutcomePending
	}

	return OutcomeFail
}

// RunnersUp are the intents NLU considered besides the one it chose, best first, at most three.
func (r Result) RunnersUp() []string {
	if r.Got == nil {
		return nil
	}

	var out []string

	for _, in := range r.Got.Considered {
		if len(out) == runnersUp {
			break
		}

		if in.Name != "" {
			out = append(out, in.Name)
		}
	}

	return out
}
