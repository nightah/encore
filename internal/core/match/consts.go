package match

import "math"

// Decisions.
const (
	DecisionNotFound Decision = iota
	DecisionAsk
	DecisionPlay
)

// Retriever names for Config.Retriever.
const (
	RetrieverInverted = "inverted"
	RetrieverTrigram  = "trigram"
)

// Inverted index fields.
const (
	termWords termField = iota
	termPhonetic
	termTrigrams
	termFields
)

// Inverted index layout.
const (
	slotBits  = 28
	slotMask  = 1<<slotBits - 1
	slotLimit = 1 << slotBits
	// tfOverflow in a posting means the term frequency is in freqs.
	tfOverflow = 1<<(32-slotBits) - 1
	// Tombstoned slots are compacted once there are at least compactMinDead of them and they are 1/compactRatio of all slots.
	compactMinDead = 1024
	compactRatio   = 4
	// writeChunk is how many Docs a write changes per hold of the write lock.
	writeChunk = 256
	// compactChunk is how many slots or terms a compaction reads per hold of the read lock.
	compactChunk = 4096
	// A compaction catches up with concurrent writes without the write lock for up to catchUpRounds rounds, until a round has at most catchUpOps writes to replay; the rest are replayed under the write lock.
	catchUpRounds = 8
	catchUpOps    = 64
	// droppedSlot marks a slot a compaction leaves out.
	droppedSlot = math.MaxUint32
)

// Query clause boosts: exact-ish words count most, sound-alikes next, shared trigrams least.
const (
	boostNorm  = 3
	boostPhon  = 2
	boostNgram = 1
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
	// stackRunes is the longest string, in runes, that the edit distances handle without allocating.
	stackRunes = 64
	// maxFused is the longest token, in bytes, that is tried as a number written as one word.
	maxFused = 64
)

const (
	numUnit     numKind = iota // 0-19
	numTens                    // 20, 30, ... (and French "dix")
	numCompound                // a whole 21-99 in one word, e.g. "einundzwanzig"
	numHundred
	numThousand
	numMillion
)

const (
	stStart numState = iota
	stUnit
	stTens
	stHundred
	stThousand
	stMillion
)
