package nlu

import "time"

// Defaults for Runner.
const (
	defaultReprobes = 2
	defaultWait     = 2 * time.Second
	defaultParallel = 4
)

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
