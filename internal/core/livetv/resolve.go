package livetv

import (
	"cmp"
	"context"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/nightah/encore/internal/core/match"
	"github.com/nightah/encore/internal/core/media"
)

// Find finds the channel spoken names. A name wins over a number: "seven" is the channel named Seven. A number finds a channel by number only when no channel is named it or "channel" and it, so "nine" finds Channel 9 before the channel numbered 9.
func (g *Guide) Find(ctx context.Context, spoken string) (Found, error) {
	found, err := g.find(ctx, spoken, false)
	if err != nil || found.Decision != match.DecisionNotFound {
		return found, err
	}

	// A name nothing matched may be a channel added since the list was fetched.
	return g.find(ctx, spoken, true)
}

func (g *Guide) find(ctx context.Context, spoken string, stale bool) (Found, error) {
	channels, ix, err := g.list(ctx, stale)
	if err != nil {
		return Found{}, err
	}

	norm := ix.Normalize(spoken)
	joined := joinDigits(g.lang, norm)

	if isNumber(joined) {
		for _, name := range []string{norm, joined, "channel " + joined} {
			if c, ok := g.named(ctx, channels, name); ok {
				return Found{Decision: match.DecisionPlay, Channel: c}, nil
			}
		}

		if c, ok := g.numbered(ctx, channels, joined); ok {
			return Found{Decision: match.DecisionPlay, Channel: c}, nil
		}
	}

	res, err := g.search(ctx, ix, norm, joined)
	if err != nil {
		return Found{}, err
	}

	found := Found{Decision: res.Decision, Result: &res}

	switch res.Decision {
	case match.DecisionPlay:
		found.Channel = g.pick(ctx, channels, sameName(g.lang, res.Candidates[0].Doc.Title))
	case match.DecisionAsk:
		found.Choices = g.choices(ctx, channels, &res)
	}

	return found, nil
}

// search tries the words as heard and with their digits joined ("five oh three" as 503), keeping whichever matched better.
func (g *Guide) search(ctx context.Context, ix *match.Index, norm, joined string) (match.Result, error) {
	q := match.Query{Text: norm, Kinds: []media.Kind{media.KindChannel}}

	res, err := ix.Search(ctx, q)
	if err != nil || joined == norm {
		return res, err
	}

	q.Text = joined

	alt, err := ix.Search(ctx, q)
	if err == nil && better(alt, res) {
		return alt, nil
	}

	return res, err
}

func better(a, b match.Result) bool {
	if a.Decision != b.Decision {
		return a.Decision > b.Decision
	}

	return top(a) > top(b)
}

func top(r match.Result) float64 {
	if len(r.Candidates) == 0 {
		return 0
	}

	return r.Candidates[0].Score
}

// ByNumber finds the channel with a number. sub is the part after the point, or -1 when none was said, which prefers the channel without one.
func (g *Guide) ByNumber(ctx context.Context, major, sub int) (media.Channel, bool, error) {
	channels, _, err := g.list(ctx, false)
	if err != nil {
		return media.Channel{}, false, err
	}

	c, ok := g.byNumber(ctx, channels, major, sub)

	return c, ok, nil
}

func (g *Guide) byNumber(ctx context.Context, channels []media.Channel, major, sub int) (media.Channel, bool) {
	var hits []media.Channel

	for _, c := range channels {
		if c.Major == major && (sub < 0 || c.SubNumber == sub) {
			hits = append(hits, c)
		}
	}

	if len(hits) == 0 {
		return media.Channel{}, false
	}

	return g.pick(ctx, hits, func(media.Channel) bool { return true }), true
}

// numbered finds a channel by a spoken number: 24 as 24, and 72 as 72, else as 7.2.
func (g *Guide) numbered(ctx context.Context, channels []media.Channel, digits string) (media.Channel, bool) {
	n, err := strconv.Atoi(digits)
	if err != nil {
		return media.Channel{}, false
	}

	if c, ok := g.byNumber(ctx, channels, n, -1); ok {
		return c, true
	}

	if len(digits) >= 2 {
		major, _ := strconv.Atoi(digits[:len(digits)-1])
		sub, _ := strconv.Atoi(digits[len(digits)-1:])

		return g.byNumber(ctx, channels, major, sub)
	}

	return media.Channel{}, false
}

// named finds the channel whose whole name is norm, as "10" or "Channel 9".
func (g *Guide) named(ctx context.Context, channels []media.Channel, norm string) (media.Channel, bool) {
	same := sameName(g.lang, norm)

	var hits []media.Channel

	for _, c := range channels {
		if same(c) {
			hits = append(hits, c)
		}
	}

	if len(hits) == 0 {
		return media.Channel{}, false
	}

	return g.pick(ctx, hits, func(media.Channel) bool { return true }), true
}

// sameName matches the channels named name, ignoring case, punctuation and how numbers are written, with a name's spoken digits joined too, so "72", as Alexa writes "seven two", is 7two.
func sameName(lang, name string) func(media.Channel) bool {
	want := match.Normalize(lang, name)

	is := func(written string) bool {
		norm := match.Normalize(lang, written)

		return norm == want || joinDigits(lang, norm) == want
	}

	return func(c media.Channel) bool {
		return is(c.Name) || slices.ContainsFunc(Aliases(lang, c.Name), is)
	}
}

// pick chooses among the channels meant, of which there is always one, the same way every time: the lowest numbered, then one the guide has something on now for, then the lowest ID. The guide is asked only when channels share the lowest number.
func (g *Guide) pick(ctx context.Context, channels []media.Channel, meant func(media.Channel) bool) media.Channel {
	var hits []media.Channel

	for _, c := range channels {
		if meant(c) {
			hits = append(hits, c)
		}
	}

	slices.SortStableFunc(hits, func(a, b media.Channel) int {
		return cmp.Or(cmp.Compare(a.Major, b.Major), cmp.Compare(a.SubNumber, b.SubNumber))
	})

	tied := 1
	for tied < len(hits) && hits[tied].Major == hits[0].Major && hits[tied].SubNumber == hits[0].SubNumber {
		tied++
	}

	hits = hits[:tied]
	if len(hits) == 1 {
		return hits[0]
	}

	onNow := map[int]bool{}

	if guide, err := g.OnNow(ctx); err == nil {
		now := g.now()

		for _, c := range guide {
			onNow[c.ID] = c.Now != nil && c.Now.OnAt(now)
		}
	}

	slices.SortStableFunc(hits, func(a, b media.Channel) int {
		if onNow[a.ID] != onNow[b.ID] {
			if onNow[a.ID] {
				return -1
			}

			return 1
		}

		return cmp.Compare(a.ID, b.ID)
	})

	return hits[0]
}

// choices are the channels to ask about, one per name, in the search's order.
func (g *Guide) choices(ctx context.Context, channels []media.Channel, res *match.Result) []media.Channel {
	var out []media.Channel

	seen := map[string]bool{}

	for _, cand := range res.Candidates {
		if cand.Score < res.AskScore {
			break
		}

		key := match.Normalize(g.lang, cand.Doc.Title)
		if seen[key] {
			continue
		}

		seen[key] = true
		out = append(out, g.pick(ctx, channels, sameName(g.lang, cand.Doc.Title)))
	}

	return out
}

// FindProgramme finds a programme on now or next by its title. A programme on now wins over the same programme on next, and among channels showing it the usual pick decides.
func (g *Guide) FindProgramme(ctx context.Context, spoken string) (Programme, bool, error) {
	guide, err := g.OnNow(ctx)
	if err != nil {
		return Programme{}, false, err
	}

	now := g.now()

	var docs []match.Doc

	for _, c := range guide {
		if c.Now != nil && c.Now.OnAt(now) {
			docs = append(docs, match.Doc{ID: nowPrefix + strconv.Itoa(c.ID), Kind: media.KindChannel, Ref: media.Ref{Kind: media.KindChannel, ID: c.ID}, Title: c.Now.Title, Boost: nowBoost})
		}

		if c.Next != nil && c.Next.End.After(now) {
			docs = append(docs, match.Doc{ID: nextPrefix + strconv.Itoa(c.ID), Kind: media.KindChannel, Ref: media.Ref{Kind: media.KindChannel, ID: c.ID}, Title: c.Next.Title})
		}
	}

	if len(docs) == 0 {
		return Programme{}, false, nil
	}

	ix, err := g.newIndex(docs)
	if err != nil {
		return Programme{}, false, err
	}
	defer ix.Close()

	res, err := ix.Search(ctx, match.Query{Text: spoken, Kinds: []media.Kind{media.KindChannel}})
	if err != nil || len(res.Candidates) == 0 || res.Decision == match.DecisionNotFound {
		return Programme{}, false, err
	}

	// Asked about several, it is still one programme when they all have its title, as the cricket on two channels.
	title := match.Normalize(g.lang, res.Candidates[0].Doc.Title)
	if res.Decision == match.DecisionAsk {
		for _, cand := range res.Candidates {
			if cand.Score >= res.AskScore && match.Normalize(g.lang, cand.Doc.Title) != title {
				return Programme{}, false, nil
			}
		}
	}

	// The search keeps one of a title's copies, so the channels showing it are found in the guide: those with it on now, else those with it on next.
	same := func(b *media.Broadcast) bool { return b != nil && match.Normalize(g.lang, b.Title) == title }

	var showing []media.Channel

	onNow := false

	for _, c := range guide {
		if same(c.Now) && c.Now.OnAt(now) {
			if !onNow {
				onNow, showing = true, nil
			}

			showing = append(showing, c)
		} else if !onNow && same(c.Next) && c.Next.End.After(now) {
			showing = append(showing, c)
		}
	}

	c := g.pick(ctx, showing, func(media.Channel) bool { return true })

	b := c.Next
	if onNow {
		b = c.Now
	}

	return Programme{Channel: c, Broadcast: *b, OnNow: onNow}, true, nil
}

// Aliases are other ways of writing a channel's name that a listener's words normalise to: with a space between digits and letters, so "nine go" finds 9Go! and "seven two" 7two, with a dot between letters spoken, so "racing dot com" finds Racing.com, and with a 1 after a word spoken in a language whose word for one is also its "a", which the matcher leaves as a word, so "tf un" finds TF1.
func Aliases(lang, name string) []string {
	var split, dotted strings.Builder

	runes := []rune(name)
	for i, r := range runes {
		if i > 0 && (unicode.IsDigit(runes[i-1]) && unicode.IsLetter(r) || unicode.IsLetter(runes[i-1]) && unicode.IsDigit(r)) {
			split.WriteByte(' ')
		}

		split.WriteRune(r)

		if r == '.' && i > 0 && i+1 < len(runes) && unicode.IsLetter(runes[i-1]) && unicode.IsLetter(runes[i+1]) {
			dotted.WriteString(" dot ")
		} else {
			dotted.WriteRune(r)
		}
	}

	candidates := []string{split.String(), dotted.String()}

	if words := strings.Fields(split.String()); len(words) > 1 {
		for _, one := range spokenOnes[strings.SplitN(lang, "-", 2)[0]] {
			spoken := slices.Clone(words)
			changed := false

			for i := 1; i < len(spoken); i++ {
				if spoken[i] == "1" && !isNumber(spoken[i-1]) {
					spoken[i], changed = one, true
				}
			}

			if changed {
				candidates = append(candidates, strings.Join(spoken, " "))
			}
		}
	}

	var out []string

	for _, a := range candidates {
		if a != name && !slices.Contains(out, a) {
			out = append(out, a)
		}
	}

	return out
}

// joinDigits joins runs of single spoken digits into one number, so "5 oh 3" is 503 and "7 2" is 72. In English "oh" between digits is a zero.
func joinDigits(lang, norm string) string {
	words := strings.Fields(norm)
	out := make([]string, 0, len(words))

	digit := func(w string) bool { return len(w) == 1 && w[0] >= '0' && w[0] <= '9' }
	zero := func(i int) bool {
		return (lang == "" || strings.HasPrefix(lang, "en")) && (words[i] == "oh" || words[i] == "o") && i > 0 && i+1 < len(words) && digit(words[i-1]) && digit(words[i+1])
	}

	for i := 0; i < len(words); i++ {
		if !digit(words[i]) {
			out = append(out, words[i])

			continue
		}

		run := words[i]
		for i+1 < len(words) && (digit(words[i+1]) || zero(i+1)) {
			i++
			if zero(i) {
				run += "0"
			} else {
				run += words[i]
			}
		}

		out = append(out, run)
	}

	return strings.Join(out, " ")
}

func isNumber(s string) bool {
	if s == "" {
		return false
	}

	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}

	return true
}
