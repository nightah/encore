package smapitest

import (
	"encoding/json"
	"net/http/httptest"
	"sync"
	"time"
)

// Server is the fake. Its URL serves both SMAPI and LWA.
type Server struct {
	*httptest.Server

	opts Options

	mu          sync.Mutex
	seq         int
	tokens      map[string]time.Time
	revoked     bool
	refreshes   int
	exchanges   int
	calls       map[string]int
	inject      []int
	injectN     int
	failLocales map[string]string
	// unreported are locales left out of the skill status, as when a failed import is rolled back before Amazon records a build.
	unreported map[string]bool
	// valueLimits, keyed by "<locale>/<slot type>", fail a locale's build when its model holds more values of the slot type than the limit.
	valueLimits map[string]int
	// rejected fail the build of any locale whose model has one of these slot values or synonyms.
	rejected map[string]bool
	// unimported are locales left out of an import's resources, so their builds are known only from the skill status.
	unimported  map[string]bool
	uploads     map[string][]byte
	skills      map[string]*Skill
	order       []string
	imports     map[string]*importJob
	lastImport  map[string]*importJob
	statusPolls map[string]int
	sims        map[string]*simJob
	unresolved  int
	reaches     map[string]bool
	profiles    map[string][]NLUAnswer
	profiled    map[string]int
}

// Options configure the fake.
type Options struct {
	ClientID     string
	ClientSecret string
	// RefreshToken is the refresh token the token endpoint accepts and issues.
	RefreshToken string
	// AuthCode is the authorisation code the token endpoint accepts.
	AuthCode string
	// TokenTTL is the issued access tokens' lifetime. Under ten seconds, golang.org/x/oauth2 treats a token as expired at once, so every call refreshes.
	TokenTTL time.Duration
	Vendors  []Vendor
	// Pending is how many polls an import, build or simulation stays IN_PROGRESS.
	Pending int
	// ImportWithoutModels reports only the manifest among an import's resources, so locale builds are known only from the skill status.
	ImportWithoutModels bool
	// UnderscoreLocales names locales the way Amazon's resource names do (interactionModels.en_AU), in skill status keys as well as import resources.
	UnderscoreLocales bool
	// UnresolvedSimulations is how many simulations fail as Amazon's do while a newly enabled model hasn't reached the simulator yet.
	UnresolvedSimulations int
}

// Vendor is a developer account the fake reports.
type Vendor struct {
	ID    string
	Name  string
	Roles []string
}

// Skill is the fake's view of a skill.
type Skill struct {
	ID       string
	VendorID string
	Names    map[string]string
	Manifest json.RawMessage
	Models   map[string]json.RawMessage
	Enabled  bool
	Imports  int
}

type skillPackage struct {
	names    map[string]string
	manifest json.RawMessage
	models   map[string]json.RawMessage
}

type importJob struct {
	id      string
	skillID string
	// skipped are locales whose model is unchanged since the last import, which Amazon reports SKIPPED.
	skipped map[string]bool
	polls   int
	errs    []problem
	locales []string
	failed  map[string]string
}

type problem struct {
	Code    string `json:"code,omitempty"`
	Message string `json:"message"`
}

// NLUAnswer is one profileNlu answer: the selected intent ("" for none) with its slot words and the IDs some of them resolve to, and the intents considered besides. A non-zero Status fails the call with that HTTP status instead.
type NLUAnswer struct {
	Intent       string
	Slots        map[string]string
	Resolved     map[string]string
	Considered   []string
	SessionEnded bool
	Status       int
}

type simJob struct {
	polls   int
	reached bool
	caption string
	failure string
}
