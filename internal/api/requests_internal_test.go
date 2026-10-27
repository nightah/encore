package api

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/nightah/encore/internal/store"
)

// Entries logged before every request got an outcome are reported as ok, one of the documented outcomes.
func TestSummaryOfEntryWithoutOutcome(t *testing.T) {
	assert.Equal(t, "ok", toSummary(store.RequestLogEntry{Intent: "SessionEndedRequest"}, nil).Outcome)
	assert.Equal(t, "played", toSummary(store.RequestLogEntry{Outcome: "played"}, nil).Outcome)
}
