package deploy

import "time"

// Deploy steps, in order.
const (
	StepPackage  Step = "package"
	StepVendor   Step = "vendor"
	StepSkill    Step = "skill"
	StepUpload   Step = "upload"
	StepImport   Step = "import"
	StepBuild    Step = "build"
	StepEnable   Step = "enable"
	StepSimulate Step = "simulate"
)

// Step states.
const (
	StateRunning StepState = "running"
	StateDone    StepState = "done"
	StateSkipped StepState = "skipped"
	StateFailed  StepState = "failed"
)

const unreportedBuild = "Amazon reported no build for this locale"

// missingLocalePolls is how many build polls a package locale may be absent from the skill status after the manifest has finished, before the build counts as failed (about two minutes at the default poll interval).
const missingLocalePolls = 10

// simulateTimeout bounds the smoke test, retries included; Amazon usually answers within seconds.
const simulateTimeout = 2 * time.Minute

// simulateAttempts is how often the smoke test asks before taking "did not resolve" as the answer: just after a model goes live, the simulator can take a little while to know it.
const simulateAttempts = 4
