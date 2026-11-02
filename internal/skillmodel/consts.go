package skillmodel

// MaxExpansions caps how many samples one pattern may produce, so a careless pattern can't blow the 1.5 MB interaction model limit on its own.
const MaxExpansions = 2000

// Severities, in increasing order.
const (
	Warning Severity = iota
	Error
)

// Thresholds for the sample-count rules.
const (
	minSamples         = 1
	recommendedSamples = 5
)

// InvocationKodi is the invocation choice that names the skill after Kodi rather than Encore; each locale may spell it in its own script (Locale.KodiInvocation).
const InvocationKodi = "kodi"

// ReferenceLanguage is the language every other language's responses are compared against.
const ReferenceLanguage = "en"

// InvocationPlaceholder stands for the invocation name in a locale's launch phrases.
const InvocationPlaceholder = "{invocation}"

// SlotSampleSeparator joins an intent and one of its slots in an utterances key that holds the slot's own samples ("ChooseMatch.Answer"): what people say when the skill asks for that slot, which Alexa matches only then.
const SlotSampleSeparator = "."

// delegationSkill leaves every dialog to the skill.
const delegationSkill = "SKILL_RESPONSE"
