package smapitest

// Defaults for Options.
const (
	DefaultClientID     = "amzn1.application-oa2-client.fake"
	DefaultClientSecret = "fake-secret"
	DefaultRefreshToken = "Atzr|fake-refresh" //nolint:gosec // fake credential for tests
	DefaultAuthCode     = "fake-auth-code"
	DefaultVendorID     = "M1FAKEVENDOR"
)

// UnresolvedMessage is Amazon's error for an utterance the simulator can't match, which it also gives just after a model goes live.
const UnresolvedMessage = "This utterance did not resolve to any intent in your skill. Please invoke your skill and try again with a different utterance or update your interaction model to include this utterance before testing again."
