package deploy

import (
	"encoding/json"
	"errors"

	"github.com/nightah/encore/internal/smapi"
)

// ErrBuildFailed means Amazon rejected one or more locales' interaction models; *Error.Locales says why.
var ErrBuildFailed = errors.New("deploy: interaction model build failed")

// errGaveUp stops a poll that has asked as often as it may.
var errGaveUp = errors.New("deploy: gave up waiting for the build status")

// Error is a deploy that SMAPI accepted but could not complete.
type Error struct {
	Step     Step
	Problems []smapi.Problem
	// Locales holds per-locale build errors.
	Locales map[string][]smapi.Problem
	// ImportID and ImportStatus are the failed import's ID and Amazon's last answer about it, as sent, for looking into a failure Amazon gives no reasons for.
	ImportID     string
	ImportStatus json.RawMessage
}

// VendorChoiceError means the user belongs to several developer accounts and must pick one.
type VendorChoiceError struct {
	Vendors []smapi.Vendor
}
