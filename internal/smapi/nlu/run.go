package nlu

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode"

	"golang.org/x/text/unicode/norm"

	"github.com/nightah/encore/internal/smapi"
)

// Run profiles every row of fixtures, all at once at Rate, and returns their results in fixture and row order. It returns early only when ctx ends.
func (r *Runner) Run(ctx context.Context, fixtures ...Fixture) ([]Result, error) {
	rate := r.Rate
	if rate <= 0 {
		rate = defaultRate
	}

	p := &pacer{interval: time.Duration(float64(time.Second) / rate)}

	var out []Result

	for _, f := range fixtures {
		for _, c := range f.Cases {
			out = append(out, Result{Locale: f.Locale, Case: c})
		}
	}

	sem := make(chan struct{}, maxInFlight)

	var wg sync.WaitGroup

	for i := range out {
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			wg.Wait()

			return nil, ctx.Err()
		}

		wg.Go(func() {
			defer func() { <-sem }()

			out[i] = r.one(ctx, p, out[i].Locale, out[i].Case)
		})
	}

	wg.Wait()

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return out, nil
}

// wait blocks until the next request may be sent, or ctx ends.
func (p *pacer) wait(ctx context.Context) error {
	p.mu.Lock()
	now := time.Now()

	at := p.next
	if at.Before(now) {
		at = now
	}

	p.next = at.Add(p.interval)
	p.mu.Unlock()

	t := time.NewTimer(at.Sub(now))
	defer t.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func (r *Runner) one(ctx context.Context, p *pacer, locale string, c Case) Result {
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
		if err := p.wait(ctx); err != nil {
			res.Err = err

			return res
		}

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
		res.Problems, res.Via = r.compare(locale, c, res.Got)
	}

	return res
}

// compare says how p differs from what c expects, or, when p is one of c's alternatives or Encore reads it as c expects, nothing and how it got there.
func (r *Runner) compare(locale string, c Case, p *smapi.NLUProfile) ([]string, string) {
	problems := differences(Reading{Intent: c.Intent, Slots: c.Slots}, p)
	if len(problems) == 0 {
		return nil, ""
	}

	for _, alt := range c.Also {
		if len(differences(alt, p)) == 0 {
			return nil, alt.Intent
		}
	}

	if p.Selected != nil && r.Reread != nil {
		heard := heardOf(p)

		for _, reading := range r.Reread(locale, heard) {
			for _, want := range append([]Reading{{Intent: c.Intent, Slots: c.Slots}}, c.Also...) {
				if reads(reading, want) {
					return nil, heard.Intent
				}
			}
		}
	}

	return problems, ""
}

// heardOf is the intent p selected and the words of each slot it filled.
func heardOf(p *smapi.NLUProfile) Reading {
	heard := Reading{Intent: p.Intent(), Slots: map[string]string{}}
	if p.Selected == nil {
		return heard
	}

	for name, slot := range p.Selected.Slots {
		if v := strings.TrimSpace(slot.Value); v != "" {
			heard.Slots[name] = v
		}
	}

	return heard
}

// differences says how p differs from want.
func differences(want Reading, p *smapi.NLUProfile) []string {
	got := p.Intent()
	if got != want.Intent {
		if got == "" {
			got = "no intent"
		}

		return []string{fmt.Sprintf("got %s, want %s", got, want.Intent)}
	}

	var out []string

	for _, name := range sortedKeys(want.Slots) {
		value := want.Slots[name]
		slot, ok := p.Selected.Slots[name]

		switch {
		case !ok || strings.TrimSpace(slot.Value) == "":
			out = append(out, fmt.Sprintf("slot %s is empty", name))
		case value != "" && !matches(slot, value):
			out = append(out, fmt.Sprintf("slot %s is %q, want %q", name, slot.Value, value))
		}
	}

	return out
}

// reads reports whether a reading of a query is want: the same intent, and each slot want fills holding its words.
func reads(reading, want Reading) bool {
	if reading.Intent != want.Intent {
		return false
	}

	for name, value := range want.Slots {
		got := strings.TrimSpace(reading.Slots[name])
		if got == "" || value != "" && fold(got) != fold(value) {
			return false
		}
	}

	return true
}

// matches reports whether s holds want, as words or a value or ID they resolve to, ignoring case and accents: Encore's matcher ignores both, and Amazon's es-US model drops accents ("ojos asi" for "ojos así").
func matches(s smapi.NLUSlot, want string) bool {
	if fold(s.Value) == fold(want) {
		return true
	}

	for _, v := range s.Resolved() {
		if fold(v) == fold(want) {
			return true
		}
	}

	return false
}

// fold lowercases s, takes the accents off its Latin letters and drops quotation marks, which Amazon's catalogue types put around a title they recognise ("\"Bohemian Rhapsody\""). Marks on other scripts stay, since there they tell words apart (が is not か).
func fold(s string) string {
	var b strings.Builder

	latin := false

	for _, r := range norm.NFD.String(strings.ToLower(strings.TrimSpace(s))) {
		if strings.ContainsRune(quotationMarks, r) || unicode.Is(unicode.Mn, r) && latin {
			continue
		}

		latin = unicode.Is(unicode.Latin, r)
		b.WriteRune(r)
	}

	return norm.NFC.String(b.String())
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
