package nlu

import "time"

// Defaults for Runner.
const (
	defaultReprobes = 2
	defaultWait     = 2 * time.Second
	// defaultRate keeps under profileNlu's limit, which is for a whole developer account, shared by every skill and locale: a full run at 2.5 a second had half its attempts throttled and got through about 2.3 a second, after a short burst.
	defaultRate = 2.0
	// defaultCooldown lets a throttled account's limit recover before the rows it cost are asked again.
	defaultCooldown = time.Minute
)

// maxInFlight caps the requests waiting on Amazon at once; at defaultRate and half a second a request, about one is.
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

// quotationMarks are the quotes fold drops; apostrophes stay, as they belong to words ("l'ancien").
const quotationMarks = "\"“”„«»「」『』"

// unprofiled is the intent no row may expect. Amazon sends nonsense ("purple elephants dancing on the moon") to whichever intent is closest, and its fallback sensitivity, offered only for English and German, didn't change that, so whether Encore then plays nothing is for the handler's tests.
const unprofiled = "AMAZON.FallbackIntent"
