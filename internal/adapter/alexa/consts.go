package alexa

import "time"

// MaxRequestBytes caps the request body. Alexa envelopes with APL and device context are a few kilobytes; anything near this is not Alexa.
const MaxRequestBytes = 256 << 10

// Request types Encore handles.
const (
	TypeLaunch       = "LaunchRequest"
	TypeIntent       = "IntentRequest"
	TypeSessionEnded = "SessionEndedRequest"
)

const exampleTitle = "Dune"

// Session attributes: the choices a question offered ("did you mean", a recommendation) and what its answer should do.
const (
	sessionPending  = "pending"
	sessionFollowup = "followup"
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
