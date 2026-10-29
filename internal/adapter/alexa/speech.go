package alexa

import (
	"fmt"
	"log/slog"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/text/language"
	"golang.org/x/text/language/display"

	"github.com/nightah/encore/internal/core/actions"
	"github.com/nightah/encore/internal/core/match"
	"github.com/nightah/encore/internal/core/media"
	"github.com/nightah/encore/internal/skillmodel"
)

var placeholder = regexp.MustCompile(`\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// NewSpeech loads every locale's responses and slot values from src.
func NewSpeech(src *skillmodel.Sources, log *slog.Logger) (*Speech, error) {
	s := &Speech{
		responses:   map[string]map[string]string{},
		languages:   map[string]string{},
		slotIDs:     map[string]map[string]map[string]string{},
		roomPhrases: map[string][]roomPhrase{},
		queryForms:  map[string][]queryForm{},
		log:         log,
	}

	for _, locale := range src.LocaleNames() {
		r, err := src.Responses(locale)
		if err != nil {
			return nil, err
		}

		s.responses[locale] = r
		s.languages[locale] = src.Locales[locale].Language

		types, err := src.Types(locale)
		if err != nil {
			return nil, err
		}

		s.slotIDs[locale] = slotIDs(s.languages[locale], types)

		utterances, err := src.Utterances(locale)
		if err != nil {
			return nil, err
		}

		if s.roomPhrases[locale], err = roomPhrases(s.languages[locale], utterances); err != nil {
			return nil, fmt.Errorf("locale %s: %w", locale, err)
		}

		if s.queryForms[locale], err = languageQueryForms(s.languages[locale], src.Model, utterances); err != nil {
			return nil, fmt.Errorf("locale %s: %w", locale, err)
		}
	}

	if _, ok := s.responses[fallbackLocale]; !ok {
		return nil, fmt.Errorf("fallback locale %s is missing", fallbackLocale)
	}

	return s, nil
}

// Render returns msg as plain text, or "" for a silent message.
func (s *Speech) Render(locale string, msg actions.Message) string {
	if msg.ID == "" {
		return ""
	}

	responses, ok := s.responses[locale]
	if !ok {
		locale, responses = fallbackLocale, s.responses[fallbackLocale]
	}

	text, ok := responses[msg.ID]
	if !ok && locale != fallbackLocale {
		// Not translated yet (Crowdin leaves untranslated messages out), so English is spoken, with English durations and lists.
		locale, text, ok = fallbackLocale, s.responses[fallbackLocale][msg.ID], s.responses[fallbackLocale][msg.ID] != ""
	}

	if !ok {
		s.log.Error("missing response", "locale", locale, "id", msg.ID)

		return ""
	}

	lang := s.languages[locale]

	return placeholder.ReplaceAllStringFunc(text, func(m string) string {
		name := m[1 : len(m)-1]

		if d, ok := msg.Durations[name]; ok {
			return spokenDuration(lang, d)
		}

		if t, ok := msg.Times[name]; ok {
			return spokenTime(lang, t)
		}

		if code, ok := msg.Languages[name]; ok {
			return languageName(locale, code)
		}

		if list, ok := msg.Lists[name]; ok {
			return spokenList(lang, list)
		}

		return msg.Params[name]
	})
}

// SlotID returns the ID of a custom slot type's value or synonym as heard in locale, or "" when it isn't one. Values without an ID are their own ID.
func (s *Speech) SlotID(locale, typ, heard string) string {
	ids, ok := s.slotIDs[locale]
	if !ok {
		ids = s.slotIDs[fallbackLocale]
	}

	return ids[typ][match.Normalize(s.languages[locale], heard)]
}

// RoomSplits returns the ways query can be read as "<title> <room phrase> <room>", or as the room first ("リビングで千と千尋の神隠し", "बेडरूम में शोले"), using the words locale's utterances put around {Room}, longest room first and rooms after the title before rooms before it. The caller decides which room, if any, is real.
func (s *Speech) RoomSplits(locale, query string) []RoomSplit {
	phrases, ok := s.roomPhrases[locale]
	if !ok {
		locale, phrases = fallbackLocale, s.roomPhrases[fallbackLocale]
	}

	lang := s.languages[locale]
	words := units(lang, query)

	var out []RoomSplit

	for i := 1; i < len(words); i++ {
		for _, p := range phrases {
			// A postposition that follows a leading room may also follow one said after the title ("दंगल बेडरूम में" for "[{Room} में] ..."), in a spaced language where the words show where the room starts.
			trailing := !p.first || len(p.before) == 0 && len(p.after) > 0 && !unspaced[lang]

			end, last := i+len(p.before), len(words)-len(p.after)
			if !trailing || end >= last || !wordsEqual(words[i:end], p.before) || !wordsEqual(words[last:], p.after) {
				continue
			}

			out = append(out, RoomSplit{Title: joinUnits(lang, words[:i]), Room: joinUnits(lang, words[end:last])})
		}
	}

	for _, p := range phrases {
		if !p.first || len(words) < len(p.before) || !wordsEqual(words, p.before) {
			continue
		}

		for j := len(words) - len(p.after) - 1; j > len(p.before); j-- {
			if wordsEqual(words[j:], p.after) {
				out = append(out, RoomSplit{Title: joinUnits(lang, words[j+len(p.after):]), Room: joinUnits(lang, words[len(p.before):j])})
			}
		}
	}

	return out
}

// Reinterpret reads a PlayMedia search query as the TV intent whose sample it is. Alexa can hear "play the next episode of bluey" as PlayMedia with the query "the next episode of bluey", since an AMAZON.SearchQuery slot takes any words; searched as a title, it matches nothing. It returns the intent to dispatch and the slot holding the spoken title, which may still end with a room: the query itself, or the intent's {Show}.
func (s *Speech) Reinterpret(locale string, intent Intent) (Intent, string) {
	forms, ok := s.queryForms[locale]
	if !ok {
		locale, forms = fallbackLocale, s.queryForms[fallbackLocale]
	}

	words := units(s.languages[locale], intent.Slots["Query"].Value)
	for i, w := range words {
		if o, ok := ordinals[s.languages[locale]][strings.ToLower(w)]; ok {
			words[i] = o
		}
	}

	for _, form := range forms {
		values, ok := form.match(words)
		if !ok {
			continue
		}

		out := Intent{Name: form.intent, Slots: map[string]Slot{}}
		title := ""

		for name, value := range values {
			out.Slots[name] = Slot{Name: name, Value: value}

			if !form.numbers[name] {
				title = name
			}
		}

		return out, title
	}

	return intent, "Query"
}

// ordinals maps the ordinals Alexa writes as digits in a search query to the words the samples spell out.
var ordinals = map[string]map[string]string{
	"en": {"1st": "first"},
	"nl": {"1e": "eerste", "1ste": "eerste"},
	"pt": {"1º": "primeiro", "1°": "primeiro"},
}

// queryForms collects the samples of each TV intent that start with one of PlayMedia's carrier phrases, without it and without a room, most specific first. Samples that are only a slot after the carrier, such as "play {Show}", are left out: any query would match them.
func queryForms(model skillmodel.Model, utterances map[string][]string) ([]queryForm, error) {
	return languageQueryForms("", model, utterances)
}

// languageQueryForms is queryForms in a language, whose words may be written without spaces (units) and whose carrier phrases may follow the query (verbFinal): "{Show}の次のエピソード" from "{Show}の次のエピソードを再生して" and PlayMedia's "{Query}を再生して".
func languageQueryForms(lang string, model skillmodel.Model, utterances map[string][]string) ([]queryForm, error) {
	var carriers, trailers [][]string

	for _, pattern := range utterances["PlayMedia"] {
		samples, err := skillmodel.Expand(pattern)
		if err != nil {
			return nil, err
		}

		for _, sample := range samples {
			words := units(lang, strings.ToLower(sample))

			switch {
			case len(words) > 1 && words[len(words)-1] == "{query}":
				carriers = append(carriers, words[:len(words)-1])
			case len(words) > 1 && words[0] == "{query}" && verbFinal[lang]:
				trailers = append(trailers, words[1:])
			}
		}
	}

	content := model.IntentContent()

	var out []queryForm

	for _, in := range model.Intents {
		if content[in.Name] != media.ContentTV {
			continue
		}

		numbers := map[string]bool{}

		for _, slot := range in.Slots {
			if slot.Type == "AMAZON.NUMBER" {
				numbers[slot.Name] = true
			}
		}

		for _, pattern := range utterances[in.Name] {
			samples, err := skillmodel.Expand(pattern)
			if err != nil {
				return nil, err
			}

			for _, sample := range samples {
				if form, ok := newQueryForm(in.Name, units(lang, sample), carriers, trailers, numbers); ok {
					form.unspaced = unspaced[lang]
					out = append(out, form)
				}
			}
		}
	}

	slices.SortStableFunc(out, func(a, b queryForm) int { return b.literals - a.literals })

	return out, nil
}

// newQueryForm reads a sample's words as a query form: without the carrier it starts with, or the trailer (a carrier after the query) it ends with.
func newQueryForm(intent string, words []string, carriers, trailers [][]string, numbers map[string]bool) (queryForm, bool) {
	if slices.Contains(words, "{Room}") {
		return queryForm{}, false
	}

	var rest []string

	for _, carrier := range carriers {
		if len(words) > len(carrier) && wordsEqual(words, carrier) {
			rest = words[len(carrier):]

			break
		}
	}

	for _, trailer := range trailers {
		if rest == nil && len(words) > len(trailer) && wordsEqual(words[len(words)-len(trailer):], trailer) {
			rest = words[:len(words)-len(trailer)]
		}
	}

	if rest == nil {
		return queryForm{}, false
	}

	form := queryForm{intent: intent, words: rest, numbers: numbers}

	for i, w := range form.words {
		if !isSlotRef(w) {
			form.words[i] = strings.ToLower(w)
			form.literals++
		}
	}

	return form, form.literals > 0
}

// match reads words as the form, returning each slot's words; a number slot takes one word that is a number.
func (f queryForm) match(words []string) (map[string]string, bool) {
	values := map[string]string{}

	var at func(i, j int) bool

	at = func(i, j int) bool {
		if i == len(f.words) {
			return j == len(words)
		}

		if j == len(words) {
			return false
		}

		w := f.words[i]
		if !isSlotRef(w) {
			return strings.EqualFold(words[j], w) && at(i+1, j+1)
		}

		name := w[1 : len(w)-1]
		if f.numbers[name] {
			if _, err := strconv.Atoi(words[j]); err != nil {
				return false
			}

			values[name] = words[j]

			return at(i+1, j+1)
		}

		for end := j + 1; end <= len(words); end++ {
			values[name] = strings.Join(words[j:end], " ")
			if f.unspaced {
				values[name] = strings.TrimSpace(strings.Join(words[j:end], ""))
			}

			if at(i+1, end) {
				return true
			}
		}

		return false
	}

	return values, at(0, 0)
}

func isSlotRef(word string) bool {
	return len(word) > 2 && word[0] == '{' && word[len(word)-1] == '}'
}

// roomPhrases collects the words around {Room} in the optional groups of utterances, such as "in the" and "on the" before a trailing room from "[(in|on) the {Room}]", and "で" after a leading one from "[{Room}で]{Movie}を再生して".
func roomPhrases(lang string, utterances map[string][]string) ([]roomPhrase, error) {
	seen := map[string]bool{}

	var out []roomPhrase

	for _, intent := range slices.Sorted(maps.Keys(utterances)) {
		for _, pattern := range utterances[intent] {
			before, after, place := roomAround(pattern)
			if place == roomNone {
				continue
			}

			leads, err := expandOrEmpty(before)
			if err != nil {
				return nil, err
			}

			trails, err := expandOrEmpty(after)
			if err != nil {
				return nil, err
			}

			for _, b := range leads {
				for _, a := range trails {
					p := roomPhrase{before: units(lang, strings.ToLower(b)), after: units(lang, strings.ToLower(a)), first: place == roomFirst}
					if key := fmt.Sprint(p); !seen[key] && len(p.before)+len(p.after) > 0 {
						seen[key] = true
						out = append(out, p)
					}
				}
			}
		}
	}

	return out, nil
}

// expandOrEmpty is Expand, with nothing at all expanding to the empty phrase.
func expandOrEmpty(pattern string) ([]string, error) {
	if strings.TrimSpace(pattern) == "" {
		return []string{""}, nil
	}

	return skillmodel.Expand(pattern)
}

// roomAround returns the pattern text around {Room} in the optional group holding it, and where the group is: before and after {Room} inside a group that ends the pattern ("(in|on) the" for "... [(in|on) the {Room}]"), inside one that starts it ("で" for "[{Room}で]{Movie}を再生して"), or, for a group in the middle, from the slot before the group to the end of the group ("を" and "で" for "{Movie}を[{Room}で]再生して"), where a search query ends as the carrier phrase begins.
func roomAround(pattern string) (before, after string, place roomPlace) {
	at := strings.LastIndex(pattern, "{Room}")
	if at < 0 {
		return "", "", roomNone
	}

	open, closing := -1, -1

	for i, depth := at-1, 0; i >= 0 && open < 0; i-- {
		switch pattern[i] {
		case ']':
			depth++
		case '[':
			if depth == 0 {
				open = i
			}

			depth--
		}
	}

	for i, depth := at+len("{Room}"), 0; i < len(pattern) && closing < 0; i++ {
		switch pattern[i] {
		case '[':
			depth++
		case ']':
			if depth == 0 {
				closing = i
			}

			depth--
		}
	}

	if open < 0 || closing < 0 {
		return "", "", roomNone
	}

	before, after = pattern[open+1:at], pattern[at+len("{Room}"):closing]
	head, tail := pattern[:open], pattern[closing+1:]

	switch {
	case strings.TrimSpace(tail) == "":
		return before, after, roomLast
	case strings.TrimSpace(head) == "":
		return before, after, roomFirst
	}

	slot := strings.LastIndex(head, "}")
	if slot < 0 {
		return "", "", roomNone
	}

	return head[slot+1:] + before, after, roomMiddle
}

// roomGroup returns what comes before {Room} inside the optional group that ends a pattern with it, "(in|on) the" for "... [(in|on) the {Room}]".
func roomGroup(pattern string) (string, bool) {
	before, after, place := roomAround(pattern)
	if before = strings.TrimSpace(before); place != roomLast || after != "" || before == "" {
		return "", false
	}

	return before, true
}

// units splits text into the units the adapter compares utterances and queries in: its words, or in a language written without spaces (unspaced) its characters, with a run of digits and a slot reference each one unit.
func units(lang, text string) []string {
	if !unspaced[lang] {
		return strings.Fields(text)
	}

	var out []string

	for i := 0; i < len(text); {
		r, size := utf8.DecodeRuneInString(text[i:])
		n := size

		switch {
		case r >= '0' && r <= '9':
			n = len(text[i:]) - len(strings.TrimLeft(text[i:], "0123456789"))
		case r == '{':
			if end := strings.IndexByte(text[i:], '}'); end > 0 {
				n = end + 1
			}
		}

		out = append(out, text[i:i+n])
		i += n
	}

	return out
}

// joinUnits joins units back into text.
func joinUnits(lang string, words []string) string {
	if unspaced[lang] {
		return strings.TrimSpace(strings.Join(words, ""))
	}

	return strings.Join(words, " ")
}

func wordsEqual(words, phrase []string) bool {
	for i, w := range phrase {
		if !strings.EqualFold(words[i], w) {
			return false
		}
	}

	return true
}

func slotIDs(lang string, types map[string][]skillmodel.SlotValue) map[string]map[string]string {
	out := make(map[string]map[string]string, len(types))

	for typ, values := range types {
		ids := map[string]string{}

		for _, v := range values {
			id := v.ID
			if id == "" {
				id = v.Value
			}

			for _, word := range append([]string{v.Value}, v.Synonyms...) {
				if k := match.Normalize(lang, word); k != "" {
					if _, taken := ids[k]; !taken {
						ids[k] = id
					}
				}
			}
		}

		out[typ] = ids
	}

	return out
}

// spokenTime words a clock time as people say it: twelve-hour with am or pm in English, twelve-hour after the part of the day and before "बजे" in Hindi ("रात 9:30 बजे"), hours and minutes in Japanese ("15時4分", "15時"), and 24-hour elsewhere.
func spokenTime(lang string, t time.Time) string {
	switch {
	case lang == "en":
		return t.Format("3:04 pm")
	case lang == "hi":
		return hindiDayPart(t.Hour()) + " " + t.Format("3:04") + " बजे"
	case lang == "ja" && t.Minute() == 0:
		return strconv.Itoa(t.Hour()) + "時"
	case lang == "ja":
		return strconv.Itoa(t.Hour()) + "時" + strconv.Itoa(t.Minute()) + "分"
	}

	return t.Format("15:04")
}

// hindiDayPart is the part of the day Hindi names before a twelve-hour time: सुबह (morning) from 4, दोपहर (afternoon) from noon, शाम (evening) from 4 pm and रात (night) from 8 pm.
func hindiDayPart(hour int) string {
	switch {
	case hour >= 4 && hour < 12:
		return "सुबह"
	case hour >= 12 && hour < 16:
		return "दोपहर"
	case hour >= 16 && hour < 20:
		return "शाम"
	default:
		return "रात"
	}
}

// languageName names a language code (BCP 47 or ISO 639-2) in the locale's language, e.g. "fre" is "French" in English and "Französisch" in German.
func languageName(locale, code string) string {
	base, err := language.ParseBase(code)
	if err != nil {
		tag, terr := language.Parse(code)
		if terr != nil {
			return code
		}

		base, _ = tag.Base()
	}

	listener, err := language.Parse(locale)
	if err != nil {
		listener = language.English
	}

	if name := display.Languages(listener).Name(base); name != "" {
		return name
	}

	return code
}

var listAnd = map[string]string{"en": "and", "de": "und", "fr": "et", "es": "y", "it": "e", "ar": "و", "hi": "और", "ja": "と", "nl": "en", "pt": "e"}

// listCommas are the list separators of languages that don't write ", ".
var listCommas = map[string]string{"ar": "، ", "ja": "、"}

// listAndJoins report, for the languages that write "and" joined to the word after it, whether it joins the last item of a list.
var listAndJoins = map[string]func(item string) bool{"ar": startsArabic}

// unspaced lists the languages written without spaces: their lists are joined with "、" and their "and" with no spaces around ("A、BとC"), their durations have no spaces ("1時間12分"), and the adapter compares their utterances character by character (units).
var unspaced = map[string]bool{"ja": true}

// verbFinal lists the languages that put the verb last, so PlayMedia's carrier phrase follows the search query ("{Query}を再生して", "{Query} चलाओ").
var verbFinal = map[string]bool{"hi": true, "ja": true}

func spokenList(lang string, items []string) string {
	and, ok := listAnd[lang]
	if !ok {
		and = listAnd["en"]
	}

	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	}

	comma, ok := listCommas[lang]
	if !ok {
		comma = ", "
	}

	last := items[len(items)-1]

	switch join := listAndJoins[lang]; {
	case unspaced[lang]:
		return strings.Join(items[:len(items)-1], comma) + and + last
	case join == nil || !join(last):
		and += " "
	}

	return strings.Join(items[:len(items)-1], comma) + " " + and + last
}

// durationWords holds the words for spoken durations per language: hour, hours, minute, minutes, and the phrase for under a minute.
var durationWords = map[string][5]string{
	"en": {"hour", "hours", "minute", "minutes", "less than a minute"},
	"de": {"Stunde", "Stunden", "Minute", "Minuten", "weniger als eine Minute"},
	"fr": {"heure", "heures", "minute", "minutes", "moins d'une minute"},
	"es": {"hora", "horas", "minuto", "minutos", "menos de un minuto"},
	"it": {"ora", "ore", "minuto", "minuti", "meno di un minuto"},
	"ar": {"ساعة", "ساعات", "دقيقة", "دقائق", "أقل من دقيقة"},
	"hi": {"घंटा", "घंटे", "मिनट", "मिनट", "एक मिनट से कम"},
	"ja": {"時間", "時間", "分", "分", "1分未満"},
	"nl": {"uur", "uur", "minuut", "minuten", "minder dan een minuut"},
	"pt": {"hora", "horas", "minuto", "minutos", "menos de um minuto"},
}

// durationCounts word a count of a duration unit in languages that don't say the number, a space and the singular for one or the plural otherwise, given the unit's singular and plural.
var durationCounts = map[string]func(n int, one, many string) string{"ar": arabicCount, "ja": unspacedCount}

// durationJoins are what joins the hours to the minutes in languages that don't put a space between them, such as Arabic's "and" and Japanese's nothing.
var durationJoins = map[string]string{"ar": " و", "ja": ""}

func spokenDuration(lang string, d time.Duration) string {
	w, ok := durationWords[lang]
	if !ok {
		w = durationWords["en"]
	}

	minutes := int(d.Round(time.Minute).Minutes())
	if minutes < 1 {
		return w[4]
	}

	unit, ok := durationCounts[lang]
	if !ok {
		unit = countUnit
	}

	var parts []string

	if h := minutes / 60; h > 0 {
		parts = append(parts, unit(h, w[0], w[1]))
	}

	if m := minutes % 60; m > 0 {
		parts = append(parts, unit(m, w[2], w[3]))
	}

	sep, ok := durationJoins[lang]
	if !ok {
		sep = " "
	}

	return strings.Join(parts, sep)
}

// countUnit words n of a unit with the singular for one and the plural for any other count, as most languages count.
func countUnit(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}

	return strconv.Itoa(n) + " " + many
}

// unspacedCount writes the number straight before the unit, as Japanese does ("1時間"); its units don't change with the count.
func unspacedCount(n int, one, _ string) string {
	return strconv.Itoa(n) + one
}
