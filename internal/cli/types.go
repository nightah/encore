package cli

import (
	"io"
	"os"
	"regexp"
	"time"

	"github.com/nightah/encore/internal/adapter/alexa"
	"github.com/nightah/encore/internal/blobstore"
	"github.com/nightah/encore/internal/config"
	"github.com/nightah/encore/internal/core/actions"
	"github.com/nightah/encore/internal/skillmodel"
	"github.com/nightah/encore/internal/smapi"
)

type dirLock struct{ f *os.File }

type restorer struct {
	cfg *config.Config
	in  io.Reader
	out io.Writer
	// interactive is whether in is a terminal a person can answer the confirmation prompt on.
	interactive bool
	yes         bool
	now         func() time.Time
}

// session is what a list or restore works with once the data directory is safe to use.
type session struct {
	// backups comes from the effective storage settings.
	backups blobstore.Store
	prefix  string
	// state is nil outside stateless mode.
	state blobstore.Store
	key   []byte
	// lock is nil in stateless mode.
	lock *dirLock
}

// voice is what both the Alexa endpoint and the admin API's request replay need.
type voice struct {
	src     *skillmodel.Sources
	speech  *alexa.Speech
	actions *actions.Service
}

// statelessRun holds what stateless mode set up, so serve can save on shutdown and tidy up.
type statelessRun struct {
	blobs blobstore.Store
	dir   string
}

// smapiFlags are how the live developer tools reach SMAPI. The endpoints are flags only so tests can point them at a fake.
type smapiFlags struct {
	skill    string
	smapiURL string
	tokenURL string
	poll     time.Duration
}

// simCases is a gen simulate cases file.
type simCases struct {
	// Locale is the locale to say the cases in, unless a case or --locale names another.
	Locale string    `yaml:"locale"`
	Cases  []simCase `yaml:"cases"`
}

// simCase is one utterance gen simulate says, and what it expects of the outcome.
type simCase struct {
	Utterance string `yaml:"utterance"`
	Locale    string `yaml:"locale"`
	// Intent, when set, must be the intent Alexa resolved the utterance to.
	Intent string `yaml:"intent"`
	// Slots must be filled. An empty value accepts any words; otherwise the words, or the value they resolve to, must equal it, ignoring case.
	Slots map[string]string `yaml:"slots"`
	// Says, when set, is a regular expression Alexa's reply must match.
	Says string `yaml:"says"`
	says *regexp.Regexp
}

// simulationRun writes gen simulate's results and tallies its checks.
type simulationRun struct {
	w              io.Writer
	json           bool
	passed, failed int
}

// simResult is one line of gen simulate --json.
type simResult struct {
	Locale     string            `json:"locale"`
	Utterance  string            `json:"utterance"`
	Simulation *smapi.Simulation `json:"simulation"`
	// Misses are how the outcome fell short of the case, when checking a file.
	Misses []string `json:"misses,omitempty"`
}
