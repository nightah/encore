// Package nlu checks how Alexa's NLU reads Encore's skill: it profiles fixture utterances through SMAPI's profileNlu against a deployed skill's built models and reports each one whose intent or slots differ from what the fixture expects.
//
// Fixtures live one file per locale (testdata/nlu/<locale>.yaml at the repository root). A row may be marked pending when the model that should make it pass is not deployed yet: it is reported, but only as a failure once it is no longer pending. An utterance that selects no intent is asked again before that counts, since Amazon's NLU now and then answers none for a moment. Profiling needs a skill and SMAPI credentials, so it runs as a developer tool, not in CI.
package nlu
