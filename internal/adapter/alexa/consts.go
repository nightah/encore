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

// progressiveTimeout bounds a progressive response so it never eats into Alexa's eight-second budget for the real answer.
const progressiveTimeout = time.Second

const fallbackLocale = "en-US"
