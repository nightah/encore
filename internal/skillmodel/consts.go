package skillmodel

// MaxExpansions caps how many samples one pattern may produce, so a careless pattern can't blow the 1.5 MB interaction model limit on its own.
const MaxExpansions = 2000

// Severities, in increasing order.
const (
	Warning Severity = iota
	Error
)

// Thresholds for the sample-count rules.
const (
	minSamples         = 1
	recommendedSamples = 5
)

// ReferenceLanguage is the language every other language's responses are compared against.
const ReferenceLanguage = "en"
