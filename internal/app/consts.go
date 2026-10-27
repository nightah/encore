package app

import (
	"time"

	"github.com/nightah/encore/internal/backend/kodi"
)

// probeTimeout bounds each question InstanceStatus asks Kodi, so a sleeping host cannot stall the admin API.
const probeTimeout = 2 * time.Second

const testTimeout = 4 * time.Second

const (
	inconclusive verdict = iota
	mirrored
	missed
)

const resubscribeDelay = 5 * time.Second

// Library group detection thresholds.
const (
	fingerprintSample       = kodi.DefaultFingerprintSample
	minFingerprintItems     = 20
	minFingerprintAgreement = 0.95
)

const (
	originConfig   = "config"
	originUser     = "user"
	originDetected = "detected"
)

// Ranking hints for the title index.
const (
	inProgressBoost    = 0.03
	recentlyAddedBoost = 0.02
	recentlyAddedFor   = 14 * 24 * time.Hour
)

// wakeBudget is how long a voice request waits for a woken Kodi. Alexa allows about eight seconds per response, so this leaves room for the action itself.
const wakeBudget = 5 * time.Second

// learnMACDelay gives a new connection time to settle before the MAC address is read for Wake-on-LAN.
const learnMACDelay = 5 * time.Second

// recorderBuffer is how many entries may wait for the database before new ones are dropped. Recording must never slow down a voice response.
const recorderBuffer = 64

const maxLoggedCandidates = 5

// Custom slot types filled from Encore's own data.
const (
	roomSlot     = "KODI_ROOM"
	playlistSlot = "KODI_PLAYLIST"
)

// snapshotTimeout bounds one snapshot read or write, so a slow blob store can't hold up start-up or shutdown for long.
const snapshotTimeout = 30 * time.Second
