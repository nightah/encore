package alexa

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/nightah/encore/internal/core/actions"
	"github.com/nightah/encore/internal/core/match"
	"github.com/nightah/encore/internal/core/media"
)

// Handler answers Alexa requests by calling the action service.
type Handler struct {
	Service     *actions.Service
	Speech      *Speech
	Targets     Targets
	Progressive *Progressive
	Recorder    Recorder
	// IntentContent maps intents to their content group (skillmodel.Model.IntentContent), so a request for a group the skill was set up without is declined even from an older deployment.
	IntentContent map[string]media.Content
	// Simulating reports whether Encore is running its deploy smoke test, so the simulator's requests are logged as such; nil means never.
	Simulating func() bool
	Log        *slog.Logger
	Now        func() time.Time
}

// Targets resolves which media centre a device controls.
type Targets interface {
	// Target returns the target for deviceID, or ErrUnlinked.
	Target(ctx context.Context, deviceID string) (actions.Target, error)
	// TargetForRoom returns the target for a room named in the request ("... in the bedroom"), or ErrUnknownRoom. A room without a Kodi instance falls back to the device's own target.
	TargetForRoom(ctx context.Context, deviceID, room string) (actions.Target, error)
	// DeviceName returns the name the Echo has in Encore, "" if it has none yet, recording the device as seen like Target does.
	DeviceName(ctx context.Context, deviceID string) (string, error)
}

// LogEntry is one handled request, for the request log.
type LogEntry struct {
	Time      time.Time
	RequestID string
	DeviceID  string
	Locale    string
	Type      string
	Intent    string
	Slots     map[string]string
	Target    string
	Match     *match.Result
	Speech    string
	Outcome   string
	Err       error
	Duration  time.Duration
	// Verify is the signature check, MatchTime the title matching, and KodiCalls the calls made to Kodi with their durations.
	Verify    time.Duration
	MatchTime time.Duration
	KodiCalls []media.Call
	// Simulated marks a request from Amazon's simulator during Encore's own deploy smoke test.
	Simulated bool
}

// Recorder stores request log entries. It must not block.
type Recorder interface {
	Record(ctx context.Context, e LogEntry)
}

// timedMatcher adds up the time spent matching titles during one request.
type timedMatcher struct {
	actions.Matcher

	total atomic.Int64
}

// Speech renders actions.Message values in the listener's locale and understands the locale's custom slot values.
type Speech struct {
	responses map[string]map[string]string
	languages map[string]string
	// slotIDs maps locale, slot type and normalised value or synonym to the value's ID.
	slotIDs map[string]map[string]map[string]string
	// roomPhrases holds, per locale, the words its utterances put between another slot and {Room} ("in the", "im"), lowercased and split into words.
	roomPhrases map[string][][]string
	log         *slog.Logger
}

// RoomSplit is one way of reading a search query as a title followed by a room.
type RoomSplit struct {
	Title string
	Room  string
}

type slots struct {
	intent Intent
	locale string
	speech *Speech
}

// Progressive sends interim speech ("Waking the bedroom TV…") through the Progressive Response API while a slow action runs.
type Progressive struct {
	client *http.Client
}

// RequestVerifier checks that a request body and its headers were signed by Alexa. *alexaverify.Verifier implements it; tests substitute their own.
type RequestVerifier interface {
	Verify(ctx context.Context, body []byte, hdr http.Header) error
}

// BodyHandler handles a request whose body has been read and verified.
type BodyHandler func(w http.ResponseWriter, r *http.Request, body []byte)

type verifyTimeKey struct{}

// The subset of the Alexa request and response JSON Encore uses. Amazon publishes no schema for these; the field names follow the custom skill JSON interface reference.

// RequestEnvelope is an incoming Alexa request.
type RequestEnvelope struct {
	Version string  `json:"version"`
	Session Session `json:"session"`
	Context Context `json:"context"`
	Request Request `json:"request"`
}

// Session is present on requests that are part of a skill session.
type Session struct {
	New        bool           `json:"new"`
	SessionID  string         `json:"sessionId"`
	Attributes map[string]any `json:"attributes,omitempty"`
}

// Context carries device and API access details.
type Context struct {
	System System `json:"System"`
}

// System identifies the device and how to call Alexa APIs on its behalf.
type System struct {
	Device struct {
		DeviceID            string                     `json:"deviceId"`
		SupportedInterfaces map[string]json.RawMessage `json:"supportedInterfaces"`
	} `json:"device"`
	APIEndpoint    string `json:"apiEndpoint"`
	APIAccessToken string `json:"apiAccessToken"`
}

// Request is the request body.
type Request struct {
	Type      string `json:"type"`
	RequestID string `json:"requestId"`
	Timestamp string `json:"timestamp"`
	Locale    string `json:"locale"`
	Intent    Intent `json:"intent"`
	Reason    string `json:"reason,omitempty"`
}

// Intent is the recognised intent and its slots.
type Intent struct {
	Name  string          `json:"name"`
	Slots map[string]Slot `json:"slots"`
}

// Slot is one filled (or empty) slot.
type Slot struct {
	Name        string       `json:"name"`
	Value       string       `json:"value"`
	Resolutions *Resolutions `json:"resolutions,omitempty"`
}

// Resolutions is Alexa's entity resolution for a slot.
type Resolutions struct {
	PerAuthority []struct {
		Authority string `json:"authority"`
		Status    struct {
			Code string `json:"code"`
		} `json:"status"`
		Values []struct {
			Value struct {
				Name string `json:"name"`
				ID   string `json:"id"`
			} `json:"value"`
		} `json:"values"`
	} `json:"resolutionsPerAuthority"`
}

// ResolvedIDs returns the IDs Alexa matched the slot to, across static and dynamic authorities.
func (s Slot) ResolvedIDs() []string {
	if s.Resolutions == nil {
		return nil
	}

	var ids []string

	for _, a := range s.Resolutions.PerAuthority {
		if a.Status.Code != "ER_SUCCESS_MATCH" {
			continue
		}

		for _, v := range a.Values {
			if v.Value.ID != "" {
				ids = append(ids, v.Value.ID)
			}
		}
	}

	return ids
}

// ResponseEnvelope is Encore's reply.
type ResponseEnvelope struct {
	Version           string         `json:"version"`
	SessionAttributes map[string]any `json:"sessionAttributes,omitempty"`
	Response          Response       `json:"response"`
}

// Response is the response body.
type Response struct {
	OutputSpeech     *OutputSpeech `json:"outputSpeech,omitempty"`
	Card             *Card         `json:"card,omitempty"`
	Reprompt         *Reprompt     `json:"reprompt,omitempty"`
	ShouldEndSession *bool         `json:"shouldEndSession,omitempty"`
}

// OutputSpeech is what Alexa says.
type OutputSpeech struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
	SSML string `json:"ssml,omitempty"`
}

// Reprompt is said if the user doesn't answer a question.
type Reprompt struct {
	OutputSpeech OutputSpeech `json:"outputSpeech"`
}

// Card is shown in the Alexa app.
type Card struct {
	Type    string `json:"type"`
	Title   string `json:"title"`
	Content string `json:"content"`
}
