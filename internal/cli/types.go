package cli

import (
	"io"
	"os"
	"time"

	"github.com/nightah/encore/internal/adapter/alexa"
	"github.com/nightah/encore/internal/blobstore"
	"github.com/nightah/encore/internal/config"
	"github.com/nightah/encore/internal/core/actions"
	"github.com/nightah/encore/internal/skillmodel"
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
