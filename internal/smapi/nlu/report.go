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

// Failed reports whether any row failed outright.
func (s Summary) Failed() bool {
	return s.Fail > 0
}
