package cli

// lockFileName is the file serve holds an exclusive lock on in the data directory for as long as it runs. restore takes the same lock, so it never replaces a database a server has open, and a server can't start mid-restore. The operating system drops the lock when the process exits, however it exits.
const lockFileName = "encore.lock"

// beforeRestoreLayout timestamps the copy of the database a restore moves aside.
const beforeRestoreLayout = "20060102T150405Z"

// certURLSetting stores the URL of the certificate chain Alexa last signed a request with, so serve can warm the verifier's cache with it on start.
const certURLSetting = "alexa.cert_chain_url"

// Environment the live SMAPI developer tools (gen deploy, gen nlu, gen open) read their credentials from, as the live SMAPI tests do.
const (
	envClientID     = "SMAPI_CLIENT_ID"
	envClientSecret = "SMAPI_CLIENT_SECRET" //nolint:gosec // an environment variable's name, not a credential
	envRefreshToken = "SMAPI_REFRESH_TOKEN" //nolint:gosec // as above
	envSkillID      = "SMAPI_SKILL_ID"
	envVendorID     = "SMAPI_VENDOR_ID"
	envLibrarySkill = "SMAPI_LIBRARY_SKILL_ID"
)

// ciSkillName is the only skill name gen deploy deploys to, in every locale the skill has a name in, so it can never replace the skill Encore deploys for a user. Encore looks its own skill up by the name it gives it, "Encore", so it never adopts this one either.
const ciSkillName = "Encore NLU CI"

// standInTypes are the slot types Encore fills from a library (app.SlotValues): the built-in title types it extends, and its own music video and playlist types. gen deploy fills them with the titles the NLU fixtures ask for, so the skill reads them as it would with a library that has them.
var standInTypes = []string{"AMAZON.Movie", "AMAZON.TVSeries", "AMAZON.Musician", "AMAZON.MusicAlbum", "KODI_MUSIC_VIDEO", "KODI_PLAYLIST"}

// ciEndpoint is the endpoint gen deploy gives the skill. Profiling and the simulator's launch checks never reach it.
const ciEndpoint = "https://encore.invalid/api/v1/alexa"

// defaultNLUFixtures is where gen nlu finds its fixtures when run from the repository root, as make nlu does.
const defaultNLUFixtures = "testdata/nlu"
