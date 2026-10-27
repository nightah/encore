package match

// Decisions.
const (
	DecisionNotFound Decision = iota
	DecisionAsk
	DecisionPlay
)

// Retriever names for Config.Retriever.
const (
	RetrieverBleve   = "bleve"
	RetrieverTrigram = "trigram"
)

// Index fields. Docs carry every alias form in each text field.
const (
	fieldNorm  = "title_norm"
	fieldNgram = "title_ngram"
	fieldPhon  = "title_phon"
	fieldKind  = "kind"
)

// Query clause boosts: exact-ish words count most, sound-alikes next, shared trigrams least.
const (
	boostNorm  = 3
	boostPhon  = 2
	boostNgram = 1
	batchSize  = 2000
	// fuzzyPrefix pins the first letter of fuzzy words, which keeps term expansion cheap; first-letter errors are left to the phonetic clause.
	fuzzyPrefix = 1
	// ngramFallbackHits is how few word and phonetic hits trigger the trigram clause.
	ngramFallbackHits = 10
)

// Weights of the blended score.
const (
	wSet      = 0.45
	wJaro     = 0.25
	wOSA      = 0.15
	wPhonetic = 0.15
	// partialFactor discounts partial_ratio so a fragment never outranks the whole title.
	partialFactor = 0.9
	// homophoneSet is the set score given to strings that sound identical with spaces removed.
	homophoneSet = 0.9
	// nonExactCap keeps fuzzy matches strictly below an exact one.
	nonExactCap = 0.99
	// digitWeight is how many characters a number token counts for in coverage: numbers tell sequels apart.
	digitWeight = 4
)

const (
	numUnit     numKind = iota // 0-19
	numTens                    // 20, 30, ... (and French "dix")
	numCompound                // a whole 21-99 in one word, e.g. "einundzwanzig"
	numHundred
	numThousand
)

const (
	stStart numState = iota
	stUnit
	stTens
	stHundred
	stThousand
)
