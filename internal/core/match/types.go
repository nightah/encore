package match

import (
	"context"
	"sync"
	"unicode/utf8"

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

	// Retriever selects candidate retrieval: RetrieverInverted (default) or RetrieverTrigram for tiny libraries.
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

// Retriever is candidate retrieval supplied from outside the package through NewWithRetriever, for the benchmarks module to compare other implementations with the in-tree inverted index. Encore never uses one.
type Retriever interface {
	// Upsert adds docs, replacing any with the same ID.
	Upsert(docs []RetrievalDoc) error
	// Remove deletes Docs by ID; unknown IDs are ignored.
	Remove(ids []string) error
	// Candidates returns up to k Doc IDs likely to match q, best first, restricted to kinds when non-empty.
	Candidates(ctx context.Context, q RetrievalQuery, kinds []media.Kind, k int) ([]string, error)
	// Close releases the retriever.
	Close() error
}

// RetrievalDoc is what a Retriever indexes for one Doc: the distinct normalised words of all its aliases, and each alias with spaces removed.
type RetrievalDoc struct {
	ID       string
	Kind     media.Kind
	Words    []string
	Compacts []string
}

// RetrievalQuery is what a Retriever searches with: the query words worth searching for (stop words left out unless that is all there is), the distinct phonetic keys of those words and of the whole query, and the query with spaces removed, for trigrams.
type RetrievalQuery struct {
	Terms   []RetrievalTerm
	Keys    []string
	Compact string
}

// RetrievalTerm is one query word and how many edits a fuzzy match of it may make; zero means it must match exactly. Fuzzy matches keep the word's first rune.
type RetrievalTerm struct {
	Text      string
	Fuzziness int
}

// externalRetriever adapts a Retriever to the index.
type externalRetriever struct {
	lang *language
	r    Retriever
}

// invertedRetriever is an in-memory inverted index over words, phonetic keys and trigrams, ranked by TF-IDF with coordination factors. Replaced or deleted Docs are tombstoned and leave the document counts at once; once they are a quarter of the slots, a compaction in the background rebuilds the postings without them.
type invertedRetriever struct {
	mu   sync.RWMutex
	lang *language
	postingSet
	// live counts live slots.
	live int
	// compacting is the compaction under way, if any.
	compacting *compaction
	// maxSlots is slotLimit, lowered in tests.
	maxSlots int
	// background runs compactions, tracked by wg; tests replace it to run them when they choose.
	background func(func())
	wg         sync.WaitGroup
	pool       sync.Pool
}

// postingSet is the part of the inverted index that compaction rebuilds.
type postingSet struct {
	slots []invertedSlot
	byID  map[string]uint32
	// dead counts tombstoned slots.
	dead   int
	fields [termFields]postingField
	// buckets groups word term IDs by first rune and length in runes, for fuzzy expansion.
	buckets map[wordBucket][]uint32
	// freqs holds term frequencies of tfOverflow and above, which do not fit in a posting.
	freqs map[freqKey]uint32
}

// compaction is a postingSet being rebuilt without tombstones while the index stays in use. Kills made meanwhile are logged in it, and it replays them and the slots added since it began before it is swapped in.
type compaction struct {
	// base is how many slots there were when it began.
	base uint32
	set  postingSet
	// remap maps the index's slots to set's, droppedSlot for those left out.
	remap []uint32
	// killed logs the index's slots killed since it began, appended under the index's write lock; replayed is how many of them it has applied.
	killed   []uint32
	replayed int
	// done is closed once it is swapped in.
	done chan struct{}
}

// termSet is an entry's terms per field.
type termSet [termFields][]string

// termCache holds the terms of entries a write is about to replace or delete.
type termCache map[*entry]*termSet

type invertedSlot struct {
	entry *entry
	alive bool
	// norms are 1/sqrt(number of terms) per field.
	norms [termFields]float32
}

// termField is one of the inverted index's fields.
type termField uint8

// postingField maps terms to IDs and IDs to postings. A posting is a slot in its low slotBits bits and the term's frequency in that slot, capped at tfOverflow, above them; postings are in slot order.
type postingField struct {
	ids   map[string]uint32
	terms []string
	post  [][]uint32
	// live counts the live slots that have each term.
	live []int32
}

type wordBucket struct {
	first rune
	runes int
}

type freqKey struct {
	field termField
	term  uint32
	slot  uint32
}

// queryClause is one disjunct of the retrieval query: its terms are alternatives (fuzzy expansions of a word, or a query's trigrams), and total is how many there are, for the coordination factor.
type queryClause struct {
	field termField
	boost float64
	terms []clauseTerm
	total int
}

type clauseTerm struct {
	id     uint32
	weight float64
	// count is how many of the clause's alternatives are this term, as when a trigram repeats in the query.
	count int
}

type slotHit struct {
	slot  uint32
	score float64
}

// topHits is a min-heap of the best hits so far.
type topHits []slotHit

// accumulator holds per-slot scores for one search; clauseScore and clauseCount collect one clause's alternatives before they are folded in.
type accumulator struct {
	score         []float64
	matched       []uint32
	touched       []uint32
	clauseScore   []float64
	clauseCount   []uint32
	clauseTouched []uint32
}

// trigramRetriever is a dependency-free inverted index over character trigrams and phonetic keys. It is meant for small libraries and as a cross-check for the inverted index; deleted slots are tombstoned, not reclaimed.
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
	// soundAlike is set when the alias won on sound alone: the strings sound identical but their text is not close enough to score as well.
	soundAlike bool
	// numbersDiffer is set when the query carries a number and the alias does not carry the same ones ("crocodile dundee 2" against "Crocodile Dundee"): a missing sequel, which may be asked about but never played. A title's own number the query leaves out ("star wars a new hope") does not count.
	numbersDiffer bool
	bd            Breakdown
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
	code string
	and  string
	// half is how the language says "and a half", which "½" reads as ("8½" -> "8 e mezzo").
	half     string
	articles map[string]bool
	// articlePrefixes are articles written fused to the word, as Arabic writes "ال" ("الملك" -> "ملك", with "وال" and "بال" for "and the" and "with the"). Each token loses the longest one it starts with, as long as at least articlePrefixMin runes (and never fewer than one) remain, so a short word that merely starts the same way survives. Whole-word articles stay in articles.
	articlePrefixes  []string
	articlePrefixMin int
	// folds maps lowercase letters to what the language compares them as, applied by tokenize after NFKD and ahead of the shared letterFolds: spelling variants of one sound (Arabic "ٱ" -> "ا", "ة" -> "ه", "ى" -> "ي") or a second script for the same sounds (katakana -> hiragana). A letter whose script keeps its combining marks is recomposed after folding, so a fold of "カ" to "か" still gives "が" for "ガ".
	folds map[rune]string
	// split breaks tokenize's tokens into words for a script written without spaces (Japanese), and runs before fused articles, letter runs and whole-word articles are handled. Nil leaves the tokens as they are.
	split func(toks []string) []string
	stop  map[string]bool
	// elide splits on apostrophes ("l'amour" -> "l amour") instead of joining ("ocean's" -> "oceans").
	elide bool
	// vigesimal counts on in twenties past sixty, as French does ("soixante-dix" 70, "quatre-vingts" 80, "quatre-vingt-onze" 91).
	vigesimal bool
	nums      map[string]numWord
	// ordSuffixes are what follows the digits of an ordinal written with them ("6th", "5e", "2º" once folded to "2o").
	ordSuffixes []string
	// fused splits numbers written as one word ("millenovecentonovantadue") into the number words they are made of; elided maps a number word's shortened form inside such a word ("cent" in "centottanta") to its full form.
	fused  bool
	elided map[string]string
	// unitsFirst reads a unit, "und" and a tens as one number, as German says them ("neun und neunzig" 99).
	unitsFirst bool
	conj       map[string]bool
	parts      map[string]bool
	phonetic   phoneticFunc
	// soundAlikeLead makes a top candidate that won on sound alone (scored.soundAlike) lead by Config.PlayMargin to play, not just Config.TieMargin. It suits an encoder that merges spellings differing only in silent letters, which different titles often do.
	soundAlikeLead bool
}

// numTable builds a number vocabulary from ordered lists where index i means value i (units) or (i+2)*10 (tens).
type numTable struct {
	units, tens         []string
	hundreds, thousands []string
	millions            []string
	// hundredsOf are words for a whole number of hundreds ("duecento" 200).
	hundredsOf           map[string]int
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
