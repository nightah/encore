package main

import (
	"cmp"
	"maps"
	"slices"
	"strings"
	"unicode"
)

var initialisms = map[string]string{
	"Id":   "ID",
	"Uid":  "UID",
	"Url":  "URL",
	"Uri":  "URI",
	"Json": "JSON",
	"Http": "HTTP",
}

// typeName converts a schema id such as "Video.Fields.Movie" or "Library.Id" to an exported Go identifier ("VideoFieldsMovie", "LibraryID").
func typeName(id string) string {
	var b strings.Builder
	for seg := range strings.SplitSeq(id, ".") {
		b.WriteString(camelWords(seg))
	}
	return ident(b.String())
}

// fieldName converts a JSON property or parameter name to an exported Go field name. Kodi's names are mostly lowercase run-ons ("tvshowid"), so besides splitting on separators a trailing "id"/"uid" is upper-cased ("TvshowID", "ChannelUID").
func fieldName(name string) string {
	s := camelWords(name)
	lower := strings.ToLower(s)
	switch {
	case lower == "id", lower == "uid":
		s = strings.ToUpper(s)
	case strings.HasSuffix(lower, "uid") && len(s) > 3 && !strings.HasSuffix(s, "UID"):
		s = s[:len(s)-3] + "UID"
	case strings.HasSuffix(lower, "id") && len(s) > 2 && !strings.HasSuffix(s, "ID"):
		s = s[:len(s)-2] + "ID"
	}
	return ident(s)
}

func constPart(v string) string {
	switch {
	case v == "":
		return "Empty"
	case strings.HasPrefix(v, "-"):
		return "Minus" + camelWords(v[1:])
	}
	return camelWords(v)
}

// camelWords splits s on non-alphanumeric runes and upper-cases the first rune of each part, keeping the remaining case (so "TVShow" survives).
func camelWords(s string) string {
	var b strings.Builder
	for part := range strings.FieldsFuncSeq(s, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		word := strings.ToUpper(part[:1]) + part[1:]
		if v, ok := initialisms[word]; ok {
			word = v
		}
		b.WriteString(word)
	}
	return b.String()
}

func ident(s string) string {
	if s == "" {
		return "X"
	}
	if r := rune(s[0]); !unicode.IsLetter(r) {
		return "V" + s
	}
	return s
}

// docComment puts the schema description, when present, in a second paragraph.
func docComment(lead, description string) string {
	doc := wrapComment(lead)
	if d := sentence(description); d != "" {
		doc += "//\n" + wrapComment(d)
	}
	return doc
}

// sentence collapses whitespace in text and ends it with a period.
func sentence(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	if text != "" && !strings.HasSuffix(text, ".") {
		text += "."
	}
	return text
}

// wrapComment wraps text at roughly 76 columns into // lines.
func wrapComment(text string) string {
	var b strings.Builder
	line := 0
	for i, w := range strings.Fields(text) {
		switch {
		case i == 0:
			b.WriteString("// ")
		case line+len(w) >= 76:
			b.WriteString("\n// ")
			line = 0
		default:
			b.WriteByte(' ')
			line++
		}
		b.WriteString(w)
		line += len(w)
	}
	b.WriteByte('\n')
	return b.String()
}

func sortedKeys[K cmp.Ordered, V any](m map[K]V) []K {
	return slices.Sorted(maps.Keys(m))
}
