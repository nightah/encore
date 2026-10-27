package match

import (
	"context"
	"sync"
	"unicode/utf8"

	"github.com/blevesearch/bleve/v2"

	"github.com/nightah/encore/internal/core/media"
)

// Doc is one searchable title.
type Doc struct {
	// ID is unique within an index, conventionally "<instance>/<kind>/<id>".
	ID   string
	Kind media.Kind
	Ref  media.Ref
	// Title is the display title; Aliases are other names it can be found by (sort title, original title, user aliases).
	Title   string
	Aliases []string
	Year    int
	// Boost is a small static ranking hint in [0, 0.05], e.g. for in-progress or recently added items.
	Boost float64
}

// Query is a search request.
type Query struct {
	// Text is what Alexa heard, unnormalised.
	Text string
	// Kinds restricts results; empty means any kind.
	Kinds []media.Kind
	// Limit caps returned candidates; zero means a sensible default.
	Limit int
	// Hints adds per-request boosts by Doc ID, e.g. Alexa resolved this title or the user mentioned "new".
	Hints map[string]float64
}

// Breakdown explains a candidate's score.
type Breakdown struct {
	Alias       string
	TokenSet    float64
	JaroWinkler float64
	OSA         float64
	Phonetic    float64
	Boost       float64
}

// Candidate is a scored match.
type Candidate struct {
	Doc       Doc
	Score     float64
	Breakdown Breakdown
}

// Decision is what the caller should do with a result.
type Decision int

// Result is a ranked search result.
type Result struct {
	// Normalized is the query after normalisation, for logs and the match tester.
	Normalized string
	// Candidates are sorted by Score, highest first, with duplicates of the same Doc merged.
	Candidates []Candidate
	Decision   Decision
	// Margin is the top score minus the runner-up's.
	Margin float64
}

// Config tunes an Index. Zero fields take the DefaultConfig value.
type Config struct {
	// PlayScore plays the top candidate outright.
	PlayScore float64
	// PlayMarginScore plays the top candidate when it also leads the runner-up by PlayMargin.
	PlayMarginScore float64
	PlayMargin      float64
	// TieMargin is the least lead that counts as a winner at all; closer than this the caller should ask, whatever the score.
	TieMargin float64
	// AskScore is the lowest top score worth asking about; below it the result is not found.
	AskScore float64

	// CandidatePool is how many candidates retrieval hands to the scorer.
	CandidatePool int
	// Limit is the default Query.Limit.
	Limit int
	// MaxBoost caps Doc.Boost plus Query.Hints for one candidate, so boosts only reorder near-ties.
	MaxBoost float64
	// CoveragePenalty scales the penalty for query words the title does not account for: the score is multiplied by 1 - CoveragePenalty*(uncovered share).
	CoveragePenalty float64
	// CompactCoverage skips the coverage penalty when the strings match this well with spaces removed ("blue y" vs "bluey").
	CompactCoverage float64
	// DerivedPenalty multiplies scores against shortened titles, such as "Dune" for "Dune: Part Two".
	DerivedPenalty float64

	// Retriever selects candidate retrieval: RetrieverBleve (default) or RetrieverTrigram for tiny libraries.
	Retriever string
}

// Index is a searchable set of Docs for one language. It is safe for concurrent use.
type Index struct {
	lang *language
	cfg  Config
	ret  retriever

	// writeMu serialises Upsert and Delete so the retriever and the maps change together.
	writeMu sync.Mutex
	mu      sync.RWMutex
	docs    map[string]*entry
	// exact maps a primary alias form to the Docs that have it.
	exact  map[string]map[string]struct{}
	closed bool
}

// yearQ is a query that ends in a plausible release year, such as "dune 2021".
type yearQ struct {
	year int
	rest form
}

// retriever narrows the library to a candidate pool cheaply. It only needs good recall: every candidate is re-scored, and its own scores are never exposed.
type retriever interface {
	upsert(entries []*entry) error
	remove(ids []string) error
	// candidates returns up to k Doc IDs likely to match q, restricted to kinds when non-empty.
	candidates(ctx context.Context, q *form, kinds []media.Kind, k int) ([]string, error)
	close() error
}

// phoneticFilter replaces each token with its phonetic keys at the same position.
type phoneticFilter struct{ fn phoneticFunc }

type bleveRetriever struct {
	lang *language
	idx  bleve.Index
}

// trigramRetriever is a dependency-free inverted index over character trigrams and phonetic keys. It is meant for small libraries and as a cross-check for bleve; deleted slots are tombstoned, not reclaimed.
type trigramRetriever struct {
	mu       sync.RWMutex
	slots    []trigramSlot
	byID     map[string]int32
	postings map[string][]int32
}

type trigramSlot struct {
	id    string
	kind  media.Kind
	alive bool
}

type scored struct {
	score float64
	exact bool
	bd    Breakdown
}

// form is a normalised string with everything the scorer needs precomputed.
type form struct {
	norm string
	toks []string
	// sorted is the tokens in sorted order, joined, for order-insensitive edit distances; empty when already in order.
	sorted  string
	keys    [][]string
	compact string
	// compactKeys are the phonetic keys of the whole string with spaces removed, which catches split or merged words ("blue y" / "bluey").
	compactKeys []string
	// spelled is the compact form before numbers were rewritten; empty when the same as compact.
	spelled string
	// digits are the number tokens concatenated in order ("blink 1 82" -> "182"), which must agree before two forms count as the same title.
	digits string
}

type alias struct {
	form
	// text is the source name, shown in Breakdown.
	text string
	// derived aliases (subtitle or qualifier dropped) are shortened titles: they score with a penalty and never count as exact.
	derived bool
}

type entry struct {
	doc     Doc
	aliases []alias
}

type numKind uint8

type numWord struct {
	val  int
	kind numKind
	// ordinal ends the number after this word ("twenty first").
	ordinal bool
	// cont marks words that double as articles ("ein", "un"), so they only count as numbers mid-number ("vingt et un").
	cont bool
}

type language struct {
	code     string
	and      string
	articles map[string]bool
	stop     map[string]bool
	// elide splits on apostrophes ("l'amour" -> "l amour") instead of joining ("ocean's" -> "oceans").
	elide    bool
	nums     map[string]numWord
	conj     map[string]bool
	parts    map[string]bool
	phonetic phoneticFunc
}

// numTable builds a number vocabulary from ordered lists where index i means value i (units) or (i+2)*10 (tens).
type numTable struct {
	units, tens          []string
	hundreds, thousands  []string
	cont                 map[string]int
	ordinals             map[string]int
	compounds            map[string]int
	extraUnits, extraTen map[string]int
}

type numState uint8

// asciiMasks holds, per byte value, the positions where it occurs in a pattern of at most 64 bytes.
type asciiMasks [utf8.RuneSelf]uint64

// phoneticFunc returns the phonetic keys of one normalised token. Digit tokens are their own key.
type phoneticFunc func(tok string) []string
