package skillmodel

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Expand turns an utterance pattern into the sample utterances it describes.
//
//	(a|b)   one of the alternatives
//	[a|b]   optionally one of the alternatives
//	{Slot}  a slot reference, copied through unchanged
//
// Groups nest. Whitespace is collapsed and the results are de-duplicated in first-seen order. A group is joined to the text around it with a space, except next to Japanese, which is written without them ("{Movie}を(再生|上映)して").
func Expand(pattern string) ([]string, error) {
	p := &parser{src: pattern}

	alts, err := p.alternatives(0)
	if err != nil {
		return nil, err
	}

	if p.pos < len(p.src) {
		return nil, p.errorf("unexpected %q", p.src[p.pos])
	}

	seen := make(map[string]bool, len(alts))
	out := make([]string, 0, len(alts))

	for _, a := range alts {
		a = strings.Join(strings.Fields(a), " ")
		if a == "" || seen[a] {
			continue
		}

		seen[a] = true
		out = append(out, a)
	}

	if len(out) == 0 {
		return nil, fmt.Errorf("pattern %q expands to nothing", pattern)
	}

	return out, nil
}

func (p *parser) errorf(format string, args ...any) error {
	return fmt.Errorf("pattern %q at offset %d: %s", p.src, p.pos, fmt.Sprintf(format, args...))
}

// alternatives parses sequences separated by '|' until a closing bracket or the end of input, which it leaves for the caller.
func (p *parser) alternatives(depth int) ([]string, error) {
	var out []string

	for {
		seq, err := p.sequence(depth)
		if err != nil {
			return nil, err
		}

		out = append(out, seq...)
		if len(out) > MaxExpansions {
			return nil, p.errorf("more than %d expansions", MaxExpansions)
		}

		if p.pos < len(p.src) && p.src[p.pos] == '|' {
			if depth == 0 {
				return nil, p.errorf("'|' outside a group")
			}

			p.pos++

			continue
		}

		return out, nil
	}
}

// sequence parses literal text and groups up to '|', ')', ']' or the end.
func (p *parser) sequence(depth int) ([]string, error) {
	results := []string{""}

	for p.pos < len(p.src) {
		c := p.src[p.pos]

		switch c {
		case '|', ')', ']':
			return results, nil
		case '(', '[':
			optional := c == '['
			closing := byte(')')

			if optional {
				closing = ']'
			}

			p.pos++

			alts, err := p.alternatives(depth + 1)
			if err != nil {
				return nil, err
			}

			if p.pos >= len(p.src) || p.src[p.pos] != closing {
				return nil, p.errorf("missing %q", closing)
			}

			p.pos++

			if optional {
				alts = append(alts, "")
			}

			results, err = p.product(results, alts)
			if err != nil {
				return nil, err
			}
		case '{':
			end := strings.IndexByte(p.src[p.pos:], '}')
			if end < 0 {
				return nil, p.errorf("unclosed slot reference")
			}

			ref := p.src[p.pos : p.pos+end+1]
			p.pos += end + 1

			results = appendAll(results, ref)
		default:
			end := strings.IndexAny(p.src[p.pos:], "|()[]{")
			if end < 0 {
				end = len(p.src) - p.pos
			}

			results = appendAll(results, p.src[p.pos:p.pos+end])
			p.pos += end
		}
	}

	return results, nil
}

func (p *parser) product(prefixes, suffixes []string) ([]string, error) {
	if len(prefixes)*len(suffixes) > MaxExpansions {
		return nil, p.errorf("more than %d expansions", MaxExpansions)
	}

	out := make([]string, 0, len(prefixes)*len(suffixes))

	for _, pre := range prefixes {
		for _, suf := range suffixes {
			if unspacedAt(pre, suf) {
				out = append(out, pre+suf)
			} else {
				out = append(out, pre+" "+suf)
			}
		}
	}

	return out, nil
}

// unspacedAt reports whether pre and suf meet at a character of a script written without spaces.
func unspacedAt(pre, suf string) bool {
	last, _ := utf8.DecodeLastRuneInString(pre)
	first, _ := utf8.DecodeRuneInString(suf)

	return unspacedRune(last) || unspacedRune(first)
}

func unspacedRune(r rune) bool {
	return r == 'ー' || unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana)
}

func appendAll(prefixes []string, s string) []string {
	for i := range prefixes {
		prefixes[i] += s
	}

	return prefixes
}
