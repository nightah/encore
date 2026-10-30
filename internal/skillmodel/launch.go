package skillmodel

import "strings"

// InvocationName is the invocation the locale's skill uses for choice: the locale's default when choice is empty, its own spelling of "kodi" for InvocationKodi, choice itself otherwise.
func (l Locale) InvocationName(choice string) string {
	switch {
	case choice == InvocationKodi && l.KodiInvocation != "":
		return l.KodiInvocation
	case choice != "":
		return choice
	}

	return l.Invocation
}

// Phrases are every launch phrase with invocation in place: Open, then Also, then Skill.
func (l Launch) Phrases(invocation string) []string {
	var out []string

	for _, p := range append(append([]string{l.Open}, l.Also...), l.Skill) {
		if p != "" {
			out = append(out, Say(p, invocation))
		}
	}

	return out
}

// Say puts invocation in place of the placeholder in a launch phrase.
func Say(phrase, invocation string) string {
	return strings.ReplaceAll(phrase, InvocationPlaceholder, invocation)
}

// OpenPhrase is what opens the skill in locale with the invocation choice (see Locale.InvocationName), or "" for a locale the sources don't have or one without an open phrase.
func (s *Sources) OpenPhrase(locale, choice string) string {
	loc, ok := s.Locales[locale]
	if !ok || loc.Launch.Open == "" {
		return ""
	}

	return Say(loc.Launch.Open, loc.InvocationName(choice))
}
