package smapi

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"golang.org/x/oauth2"

	"github.com/nightah/encore/internal/smapi/smapiclient"
)

// Options configure a Client. The zero value talks to production with default retry and poll settings.
type Options struct {
	BaseURL    string
	HTTPClient *http.Client
	Retry      RetryPolicy
	Poll       PollConfig
}

// Client calls SMAPI on behalf of one authorised user.
type Client struct {
	api    *smapiclient.Client
	upload *doer
	poll   PollConfig
}

// RetryPolicy controls retries of throttled (429) and failed (5xx) requests. Zero fields take the defaults.
type RetryPolicy struct {
	// MaxAttempts includes the first attempt. Default 6.
	MaxAttempts int
	// Base is the first backoff; it doubles per attempt up to Max. Defaults 1s and 30s.
	Base time.Duration
	Max  time.Duration
	// MaxRetryAfter caps how long a Retry-After header may make a request wait. Default 2m.
	MaxRetryAfter time.Duration
}

// PollConfig controls polling of asynchronous operations. Zero fields take the defaults: SMAPI asks clients to wait at least 5 s between polls.
type PollConfig struct {
	// Interval is the first wait; it grows by half each poll up to MaxInterval. Defaults 5s and 15s.
	Interval    time.Duration
	MaxInterval time.Duration
	// Timeout bounds the whole wait. Default 15m.
	Timeout time.Duration
}

// Vendor is a developer account the user can deploy into.
type Vendor struct {
	ID    string   `json:"id"`
	Name  string   `json:"name"`
	Roles []string `json:"roles,omitempty"`
}

// SkillSummary is a skill in a vendor's skill list.
type SkillSummary struct {
	ID           string            `json:"skillId"`
	NameByLocale map[string]string `json:"nameByLocale,omitempty"`
	Stage        string            `json:"stage,omitempty"`
	APIs         []string          `json:"apis,omitempty"`
	LastUpdated  Time              `json:"lastUpdated,omitzero"`
}

// Upload is a presigned URL to PUT a skill package to.
type Upload struct {
	URL       string `json:"uploadUrl"`
	ExpiresAt Time   `json:"expiresAt,omitzero"`
}

// Status is the state of an asynchronous SMAPI operation.
type Status string

// ImportStatus is the state of a skill package import.
type ImportStatus struct {
	Status   Status    `json:"status"`
	Errors   []Problem `json:"errors,omitempty"`
	Warnings []Problem `json:"warnings,omitempty"`
	Skill    struct {
		ID        string           `json:"skillId"`
		ETag      string           `json:"eTag"`
		Resources []ResourceStatus `json:"resources"`
	} `json:"skill"`
}

// ResourceStatus is the import state of one part of the package, e.g. the manifest or one locale's interaction model.
type ResourceStatus struct {
	Name     string    `json:"name"`
	Action   string    `json:"action,omitempty"`
	Status   Status    `json:"status"`
	Errors   []Problem `json:"errors,omitempty"`
	Warnings []Problem `json:"warnings,omitempty"`
}

// SkillStatus is the build state of a skill's manifest and per-locale interaction models.
type SkillStatus struct {
	// Raw is the response as Amazon sent it, for debugging differences from its spec.
	Raw json.RawMessage `json:"-"`

	Manifest *struct {
		ETag              string      `json:"eTag"`
		LastUpdateRequest BuildStatus `json:"lastUpdateRequest"`
	} `json:"manifest"`
	InteractionModel map[string]struct {
		ETag              string      `json:"eTag"`
		LastUpdateRequest BuildStatus `json:"lastUpdateRequest"`
	} `json:"interactionModel"`
}

// BuildStatus is the outcome of the last update to a manifest or interaction model.
type BuildStatus struct {
	Status       Status    `json:"status"`
	Errors       []Problem `json:"errors,omitempty"`
	Warnings     []Problem `json:"warnings,omitempty"`
	BuildDetails *struct {
		Steps []BuildStep `json:"steps"`
	} `json:"buildDetails,omitempty"`
}

// BuildStep is one stage of an interaction model build.
type BuildStep struct {
	Name   string    `json:"name"`
	Status Status    `json:"status"`
	Errors []Problem `json:"errors,omitempty"`
}

// Problem is one error or warning SMAPI reported, e.g. a violation, an import error or a build error.
type Problem struct {
	Code    string `json:"code,omitempty"`
	Message string `json:"message"`
}

// Simulation is the outcome of a simulated utterance.
type Simulation struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	// Invoked is true when Alexa routed the utterance to the skill's endpoint.
	Invoked bool `json:"invoked"`
	// Captions are Alexa's spoken responses.
	Captions []string `json:"captions,omitempty"`
	Error    string   `json:"error,omitempty"`
}

// Time is a timestamp in a SMAPI response. Amazon's spec documents RFC 3339 strings, but the live API also sends Unix milliseconds, as a JSON string or number (the skill upload URL's expiresAt is "1793002725519"), so both are accepted.
type Time struct {
	time.Time
}

type contextTokenSource interface {
	TokenContext(ctx context.Context) (*oauth2.Token, error)
}

// rejectingTokenSource is a token source that can be told an access token was rejected, as *lwa.TokenSource can.
type rejectingTokenSource interface {
	Rejected(accessToken string)
}

// doer adds authorisation and retries to an *http.Client.
type doer struct {
	http   *http.Client
	tokens oauth2.TokenSource // nil for presigned S3 requests, which must not carry a bearer token
	retry  RetryPolicy
}

// caller performs one generated-client request. Passing the call rather than its response keeps reading and closing the body in one place.
type caller func() (*http.Response, error)

// NLUProfile is how Alexa's NLU read one utterance against a locale's interaction model.
type NLUProfile struct {
	// SessionEnded is true when the utterance leaves the skill, e.g. Alexa's own exit or home intents.
	SessionEnded bool `json:"sessionEnded"`
	// Selected is the intent NLU chose, nil when it chose none.
	Selected *NLUIntent `json:"selectedIntent,omitempty"`
	// Considered are the intents NLU weighed but didn't choose, best first.
	Considered []NLUIntent `json:"consideredIntents,omitempty"`
}

// NLUIntent is an intent with the slots NLU filled.
type NLUIntent struct {
	Name               string             `json:"name"`
	ConfirmationStatus string             `json:"confirmationStatus,omitempty"`
	Slots              map[string]NLUSlot `json:"slots,omitempty"`
}

// NLUSlot is one slot as NLU heard it: Value is the words, Resolutions the slot type values they resolved to.
type NLUSlot struct {
	Name        string          `json:"name"`
	Value       string          `json:"value,omitempty"`
	Resolutions *NLUResolutions `json:"resolutions,omitempty"`
}

// NLUResolutions are a slot's entity resolution results.
type NLUResolutions struct {
	PerAuthority []NLUAuthority `json:"resolutionsPerAuthority"`
}

// NLUAuthority is the resolution against one source of values, such as one custom slot type.
type NLUAuthority struct {
	Authority string `json:"authority"`
	Status    struct {
		Code string `json:"code"`
	} `json:"status"`
	Values []NLUValue `json:"values"`
}

// NLUValue is one resolved value, under value as in a skill request or flat as in Amazon's spec.
type NLUValue struct {
	Name  string `json:"name,omitempty"`
	ID    string `json:"id,omitempty"`
	Value *struct {
		Name string `json:"name"`
		ID   string `json:"id"`
	} `json:"value,omitempty"`
}
