package smapi

// BaseURL is SMAPI's production endpoint.
const BaseURL = "https://api.amazonalexa.com"

// maxBody caps how much of a response is read; SMAPI responses are small, but an interaction model can approach 1.5 MB.
const maxBody = 8 << 20

// StageDevelopment is the only stage Encore deploys to.
const StageDevelopment = "development"

// Statuses reported by imports and builds.
const (
	StatusInProgress Status = "IN_PROGRESS"
	StatusSucceeded  Status = "SUCCEEDED"
	StatusFailed     Status = "FAILED"
	// StatusSkipped is an import resource that didn't change, so Amazon kept the existing one.
	StatusSkipped Status = "SKIPPED"
)

// Simulation statuses.
const (
	SimulationInProgress = "IN_PROGRESS"
	SimulationSuccessful = "SUCCESSFUL"
	SimulationFailed     = "FAILED"
)

// resolutionMatch is the entity resolution status of a slot value that matched one of its type's values.
const resolutionMatch = "ER_SUCCESS_MATCH"

// busyMessage is in the simulator's answer to a simulation it turns down because another is being processed, and busyTries how many times Say asks before giving up.
const (
	busyMessage = "conflicts with another one currently being processed"
	busyTries   = 10
)
