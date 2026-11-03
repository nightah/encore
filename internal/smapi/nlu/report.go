package nlu

import (
	"fmt"
	"io"
	"strings"
)

// Summarise counts results by outcome.
func Summarise(results []Result) Summary {
	var s Summary

	for _, r := range results {
		switch r.Outcome() {
		case OutcomePass:
			s.Pass++
		case OutcomeFail:
			s.Fail++
		case OutcomePending:
			s.Pending++
		case OutcomeFixed:
			s.Fixed++
		}
	}

	return s
}

// Write reports every row that didn't simply pass, and the passes too with verbose, then the counts. A failure names the intents NLU weighed instead.
func Write(w io.Writer, results []Result, verbose bool) error {
	for _, r := range results {
		o := r.Outcome()
		if o == OutcomePass && !verbose {
			continue
		}

		line := fmt.Sprintf("%-7s %s %q", strings.ToUpper(string(o)), r.Locale, r.Case.Utterance)

		switch {
		case r.Err != nil:
			line += ": " + r.Err.Error()
		case len(r.Problems) > 0:
			line += ": " + strings.Join(r.Problems, "; ")
			if heard := r.Heard(); heard != "" {
				line += " (heard " + heard + ")"
			}

			if up := r.RunnersUp(); len(up) > 0 {
				line += " (also considered " + strings.Join(up, ", ") + ")"
			}
		}

		switch o {
		case OutcomePending:
			line += " [pending: " + r.Case.Pending + "]"
		case OutcomeFixed:
			line += ": passes now, so drop its pending mark"
		case OutcomePass, OutcomeFail:
		}

		if r.Via != "" && len(r.Problems) == 0 {
			line += " (via " + r.Via + ")"
		}

		if r.Probes > 1 {
			line += fmt.Sprintf(" [asked %d times]", r.Probes)
		}

		if _, err := fmt.Fprintln(w, line); err != nil {
			return err
		}
	}

	s := Summarise(results)
	_, err := fmt.Fprintf(w, "%d passed, %d failed, %d pending, %d fixed\n", s.Pass, s.Fail, s.Pending, s.Fixed)

	return err
}

// Heard is the words of each slot NLU filled, as Name="words", or nothing when it filled none: what a reading that fails was given to work with.
func (r Result) Heard() string {
	if r.Got == nil {
		return ""
	}

	slots := heardOf(r.Got).Slots

	parts := make([]string, 0, len(slots))
	for _, name := range sortedKeys(slots) {
		parts = append(parts, fmt.Sprintf("%s=%q", name, slots[name]))
	}

	return strings.Join(parts, " ")
}

// Failed reports whether any row failed outright.
func (s Summary) Failed() bool {
	return s.Fail > 0
}
