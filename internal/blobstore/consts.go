package blobstore

// LocalDirName is the local backend's default directory within the data directory.
const LocalDirName = "blobs"

const maxKeyLen = 1024

// probeKeyPrefix is where Probe writes; List callers can ignore keys under it.
const probeKeyPrefix = ".encore-probe-"

// Backup object naming: prefix/encore-<UTC timestamp>.db.gz. The timestamp sorts lexically in time order.
const (
	backupNamePrefix = "encore-"
	backupNameSuffix = ".db.gz"
	backupTimeLayout = "20060102T150405.000Z"
)

// tmpPrefix marks in-flight writes; List skips them.
const tmpPrefix = ".tmp-"
