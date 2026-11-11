package alexa

import (
	"time"

	"github.com/nightah/encore/internal/core/media"
)

// MaxRequestBytes caps the request body. Alexa envelopes with APL and device context are a few kilobytes; anything near this is not Alexa.
const MaxRequestBytes = 256 << 10

// Request types Encore handles.
const (
	TypeLaunch       = "LaunchRequest"
	TypeIntent       = "IntentRequest"
	TypeSessionEnded = "SessionEndedRequest"
)

const exampleTitle = "Dune"

// chooseIntent answers a "did you mean" question: by position ({Which}) or with a title said on its own ({Answer}).
const chooseIntent = "ChooseMatch"

// answerSlot is ChooseMatch's slot for a title said on its own. A question offering several titles elicits it, so the slot's own samples, a title alone among them, are matched only while that question waits.
const answerSlot = "Answer"

// whichSlot is ChooseMatch's slot for a position or age ("the second one", "the older one").
const whichSlot = "Which"

// confirmationNone is the confirmation status of an intent or slot nobody has confirmed.
const confirmationNone = "NONE"

// elicitSlot asks Alexa to fill one slot of an intent with the next thing said.
const elicitSlot = "Dialog.ElicitSlot"

// Session attributes: the choices a question offered ("did you mean", a recommendation), what its answer should do, and the question itself, to ask again when the answer isn't one.
const (
	sessionPending  = "pending"
	sessionFollowup = "followup"
	sessionQuestion = "question"
	// sessionRoom is the room a request named, so the answer to its question plays there too.
	sessionRoom = "room"
)

// ConfirmSoundURL is the sound from Amazon's sound library that acknowledges a command with nothing to say.
const ConfirmSoundURL = "soundbank://soundlibrary/ui/gameshow/amzn_ui_sfx_gameshow_neutral_response_01"

// resolvedBoost favours titles Alexa's own entity resolution picked; small, because Alexa's guess is a prior, not the answer.
const resolvedBoost = 0.03

// RequestDeadline is how long Encore works on a voice request before answering that Kodi isn't answering. Alexa gives up after about eight seconds and says the skill didn't respond, which tells the user nothing; this leaves a second for the answer to travel back. The signature check counts against it, since Alexa waited for that too.
const RequestDeadline = 7 * time.Second

// progressiveTimeout bounds a progressive response so it never eats into Alexa's eight-second budget for the real answer.
const progressiveTimeout = time.Second

const fallbackLocale = "en-US"

// certChainURLHeader carries the URL of the certificate chain Alexa signed a request with.
const certChainURLHeader = "SignatureCertChainUrl"

// Places of a pattern's optional room group: none, ending the pattern, starting it, or between a slot and the rest.
const (
	roomNone roomPlace = iota
	roomLast
	roomFirst
	roomMiddle
)

// Stages at which VerifiedFunc rejects a request.
const (
	// RejectBody means the body couldn't be read (too large, or the connection broke), carried no timestamp that parses, or, once verified, couldn't be decoded.
	RejectBody RejectStage = "body"
	// RejectUnsigned means a signature header was missing, or the certificate chain URL wasn't one of Amazon's: the request didn't come from Alexa.
	RejectUnsigned RejectStage = "unsigned"
	// RejectTimestamp means Alexa signed the request, but its timestamp was too far from Encore's clock.
	RejectTimestamp RejectStage = "timestamp"
	// RejectSignature means the signature or its certificate chain didn't check out, or the chain couldn't be fetched.
	RejectSignature RejectStage = "signature"
	// RejectSkillID means a verified request was for another skill.
	RejectSkillID RejectStage = "skill_id"
)

// rejectStages lists every RejectStage in the order the checks run.
var rejectStages = []RejectStage{RejectBody, RejectUnsigned, RejectTimestamp, RejectSignature, RejectSkillID}

// RecentRejections is how far back Rejection.Recent counts.
const RecentRejections = 24 * time.Hour

// maxQueryForms caps the intents a search query is read as before it is searched as a title (Speech.Reinterpret), since each reading that finds nothing costs a search.
const maxQueryForms = 2

// maxLooseForms caps the loose readings of a search query (queryForm.loose), tried after the query itself.
const maxLooseForms = 1

// playlistType is the slot type of a library's playlists, whose intents a search query is read as too.
const playlistType = "KODI_PLAYLIST"

// answerIntents are the built-in intents a question's answer may be instead of a title.
var answerIntents = []string{"AMAZON.YesIntent", "AMAZON.NoIntent", "AMAZON.HelpIntent", "AMAZON.StopIntent", "AMAZON.CancelIntent"}

// continueIntent and nextEpisodeIntent are the intents "continue watching the last show" and "continue watching {Show}" belong to, and seasonIntent is "play season 2 of {Show}"'s.
const (
	continueIntent    = "ContinueWatching"
	nextEpisodeIntent = "WatchNextEpisode"
	seasonIntent      = "WatchSeason"
)

// globalSearchIntent is "search for {Query}", and querySlot the slot it and the searches of one kind fill.
const (
	globalSearchIntent = "GlobalSearch"
	querySlot          = "Query"
)

// kindSearches are the searches of one kind of title, by the kind StripKind reads off a plain search's query.
var kindSearches = map[media.Kind]string{
	media.KindMovie:  "SearchMovies",
	media.KindTVShow: "SearchTVShows",
	media.KindArtist: "SearchMusic",
	media.KindAlbum:  "SearchMusic",
}

// searchIntents are the searches, which search even while a question waits, though their Query is a title slot.
var searchIntents = map[string]bool{globalSearchIntent: true, "SearchMovies": true, "SearchTVShows": true, "SearchMusic": true}

// titleSlots are the slots that hold a title said to a title intent, which a question's answer may arrive in.
var titleSlots = []string{"Movie", "Show", "Query"}
