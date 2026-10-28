package retrieval

// Index fields. Docs carry every alias form in each text field.
const (
	fieldNorm  = "title_norm"
	fieldNgram = "title_ngram"
	fieldPhon  = "title_phon"
	fieldKind  = "kind"
)

// Values from internal/core/match as of commit 96d0dfaa.
const (
	batchSize = 2000
	// fuzzyPrefix pins the first letter of fuzzy words, as the inverted index does.
	fuzzyPrefix = 1
	// Query clause boosts: exact-ish words count most, sound-alikes next, shared trigrams least.
	boostNorm  = 3
	boostPhon  = 2
	boostNgram = 1
	// ngramFallbackHits is how few word and phonetic hits trigger the trigram clause.
	ngramFallbackHits = 10
)
