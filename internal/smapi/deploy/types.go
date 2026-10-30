package deploy

import (
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"

	"github.com/nightah/encore/internal/skillmodel"
	"github.com/nightah/encore/internal/smapi"
	"github.com/nightah/encore/internal/smapi/skillpkg"
)

// Step is one stage of a deploy, for the UI's timeline.
type Step string

// StepState is a step's progress.
type StepState string

// Event reports progress. Locales is set on build and simulate events with the per-locale state so far.
type Event struct {
	Step    Step                    `json:"step"`
	State   StepState               `json:"state"`
	Message string                  `json:"message,omitempty"`
	Locales map[string]LocaleStatus `json:"locales,omitempty"`
}

// LocaleStatus is one locale's build outcome and, if run, its smoke test.
type LocaleStatus struct {
	Status     smapi.Status      `json:"status"`
	Errors     []smapi.Problem   `json:"errors,omitempty"`
	Warnings   []smapi.Problem   `json:"warnings,omitempty"`
	Simulation *smapi.Simulation `json:"simulation,omitempty"`
}

// State is what the caller persists between deploys.
type State struct {
	VendorID string `json:"vendorId,omitempty"`
	SkillID  string `json:"skillId,omitempty"`
	// Hash is the package hash of the last fully successful deploy.
	Hash string `json:"hash,omitempty"`
	// SettingsHash is the hash of that package without library titles, so the caller can tell a settings change from new titles. The deployer carries it over unchanged.
	SettingsHash string `json:"settingsHash,omitempty"`
	// Degraded are the locales the deploy Hash stands for built with fewer slot values, which a skipped deploy reports as its own.
	Degraded map[string]Degradation `json:"degraded,omitempty"`
}

// NeedsDeploy reports whether a package with hash differs from what was last deployed.
func (s State) NeedsDeploy(hash string) bool {
	return s.SkillID == "" || s.Hash != hash
}

func (e *Error) Error() string {
	var parts []string

	for _, p := range e.Problems {
		parts = append(parts, p.String())
	}

	for _, loc := range slices.Sorted(maps.Keys(e.Locales)) {
		msgs := make([]string, 0, len(e.Locales[loc]))
		for _, p := range e.Locales[loc] {
			msgs = append(msgs, p.String())
		}

		parts = append(parts, loc+": "+strings.Join(msgs, "; "))
	}

	if len(parts) == 0 {
		parts = append(parts, "no details reported")
	}

	return fmt.Sprintf("deploy: %s failed: %s", e.Step, strings.Join(parts, "; "))
}

// Unwrap lets errors.Is match ErrBuildFailed when locales failed.
func (e *Error) Unwrap() error {
	if len(e.Locales) > 0 {
		return ErrBuildFailed
	}

	return nil
}

func (e *VendorChoiceError) Error() string {
	if len(e.Vendors) == 0 {
		return "deploy: this Amazon account has no Alexa developer account; create one at developer.amazon.com first"
	}

	return fmt.Sprintf("deploy: this Amazon account belongs to %d developer accounts; choose a vendor ID", len(e.Vendors))
}

// Request is one deploy.
type Request struct {
	// State is what the previous deploy returned; empty for a first deploy.
	State   State
	Package skillpkg.Options
	// Force deploys even when the package is unchanged.
	Force bool
	// Simulate smoke-tests the skill by opening it with the simulation API, in PrimaryLocale when that is deployed and can be simulated, else in the first locale that can.
	Simulate      bool
	PrimaryLocale string
	// Progress, if set, receives events synchronously.
	Progress func(Event)
	// Log, if set, receives each import and build status Amazon reports, at debug level.
	Log *slog.Logger
	// KeepSlots are slot types a second attempt never thins out, e.g. room names, which have to be complete to be any use.
	KeepSlots []string
}

// Result is a deploy's outcome. State is meaningful even when Deploy returns an error and should be persisted, so a retry reuses the skill.
type Result struct {
	State    State                   `json:"state"`
	Skipped  bool                    `json:"skipped"`
	Locales  map[string]LocaleStatus `json:"locales,omitempty"`
	Warnings []string                `json:"warnings,omitempty"`
	// Degraded are the locales a successful deploy built with fewer slot values than asked for.
	Degraded map[string]Degradation `json:"degraded,omitempty"`
}

// Degradation is how a locale whose model Amazon would not build with every slot value was built with fewer.
type Degradation struct {
	// Reason is what Amazon said when it would not build the locale with every value.
	Reason []smapi.Problem `json:"reason"`
	// Dropped are the values Amazon's errors named, which were left out; when it named none, every other value of each slot type not in Request.KeepSlots was.
	Dropped []string `json:"dropped,omitempty"`
	// Kept and Total count the slot values, of every type, in the locale's model and asked for.
	Kept  int `json:"kept"`
	Total int `json:"total"`
	// Bytes is the size of the locale's model as deployed.
	Bytes int `json:"bytes"`
}

// Deployer deploys the skill built from src through client.
type Deployer struct {
	client *smapi.Client
	src    *skillmodel.Sources
}

type run struct {
	d    *Deployer
	req  Request
	res  *Result
	pkg  *skillpkg.Package
	step Step
	// missing counts consecutive build polls a locale has been absent from the skill status.
	missing  map[string]int
	imported map[string]bool
	// retried is set once the package was built again with fewer slot values, which happens at most once a deploy.
	retried bool
}
