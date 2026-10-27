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
	unreported  map[string]bool
	uploads     map[string][]byte
	skills      map[string]*Skill
	order       []string
	imports     map[string]*importJob
	lastImport  map[string]*importJob
	statusPolls map[string]int
	sims        map[string]*simJob
	unresolved  int
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

type simJob struct {
	polls   int
	reached bool
	caption string
	failure string
}
