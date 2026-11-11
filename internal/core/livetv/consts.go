package livetv

import "time"

const (
	// channelTTL is how long the channel list is kept before it is fetched again.
	channelTTL = 15 * time.Minute
	// refetchAfter is how old the channel list must be before a name nothing matched fetches it again, in case the channel was added since.
	refetchAfter = time.Minute
	// guideTTL is the longest what's on now is kept; it is dropped sooner when a programme on now ends first.
	guideTTL = time.Minute
	// guideMin is the shortest, so a guide whose programmes are ending stays put for a few seconds.
	guideMin = 5 * time.Second
	// availableTTL is how long a media centre with live TV is believed to have it; availableRecheck how soon one without is asked again, as its TV service may still be starting.
	availableTTL     = 5 * time.Minute
	availableRecheck = time.Minute
)

// Doc ID prefixes in the channel and programme indexes.
const (
	channelPrefix = "channel/"
	nowPrefix     = "now/"
	nextPrefix    = "next/"
)

// nowBoost puts a programme on now ahead of the same programme on next.
const nowBoost = 0.05

// spokenOnes are, by language, the words for one that are also that language's "a", so the matcher doesn't read them as numbers; a channel's name with a 1 after a word gets an alias with each.
var spokenOnes = map[string][]string{
	"fr": {"un"},
	"it": {"uno"},
	"nl": {"een", "één"},
	"pt": {"um"},
}
