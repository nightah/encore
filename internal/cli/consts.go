package cli

// lockFileName is the file serve holds an exclusive lock on in the data directory for as long as it runs. restore takes the same lock, so it never replaces a database a server has open, and a server can't start mid-restore. The operating system drops the lock when the process exits, however it exits.
const lockFileName = "encore.lock"

// beforeRestoreLayout timestamps the copy of the database a restore moves aside.
const beforeRestoreLayout = "20060102T150405Z"

// certURLSetting stores the URL of the certificate chain Alexa last signed a request with, so serve can warm the verifier's cache with it on start.
const certURLSetting = "alexa.cert_chain_url"

// Environment the live SMAPI developer tools (gen nlu, gen open) read their credentials from, as the live SMAPI tests do.
const (
	envClientID     = "SMAPI_CLIENT_ID"
	envClientSecret = "SMAPI_CLIENT_SECRET" //nolint:gosec // an environment variable's name, not a credential
	envRefreshToken = "SMAPI_REFRESH_TOKEN" //nolint:gosec // as above
	envSkillID      = "SMAPI_SKILL_ID"
)

// defaultNLUFixtures is where gen nlu finds its fixtures when run from the repository root, as make nlu does.
const defaultNLUFixtures = "testdata/nlu"
