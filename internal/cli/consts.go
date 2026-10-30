package cli

// lockFileName is the file serve holds an exclusive lock on in the data directory for as long as it runs. restore takes the same lock, so it never replaces a database a server has open, and a server can't start mid-restore. The operating system drops the lock when the process exits, however it exits.
const lockFileName = "encore.lock"

// beforeRestoreLayout timestamps the copy of the database a restore moves aside.
const beforeRestoreLayout = "20060102T150405Z"

// certURLSetting stores the URL of the certificate chain Alexa last signed a request with, so serve can warm the verifier's cache with it on start.
const certURLSetting = "alexa.cert_chain_url"
