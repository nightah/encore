package nlu

import "time"

// Defaults for Runner.
const (
	defaultReprobes = 2
	defaultWait     = 2 * time.Second
	// defaultRate keeps under profileNlu's limit: measured at about three requests a second for a whole developer account, shared by every skill and locale, with a short burst before it throttles.
	defaultRate = 2.5
)

// maxInFlight caps the requests waiting on Amazon at once; at defaultRate and half a second a request, about two are.
const maxInFlight = 8

// runnersUp is how many considered intents a mismatch reports.
const runnersUp = 3

// Outcomes of one fixture row.
const (
	// OutcomePass is a row that profiled as expected.
	OutcomePass Outcome = "pass"
	// OutcomeFail is a row that didn't, or that couldn't be profiled.
	OutcomeFail Outcome = "fail"
	// OutcomePending is a pending row that still fails, as expected until the next deploy.
	OutcomePending Outcome = "pending"
	// OutcomeFixed is a pending row that now passes, so its pending mark can go.
	OutcomeFixed Outcome = "fixed"
)

// playMedia is the catch-all search intent, and queryslot its slot, whose words Encore reads again as the film, TV and music intents it may be.
const (
	playMedia = "PlayMedia"
	queryslot = "Query"
)
