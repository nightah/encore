// Package deploy creates or updates Encore's skill in the user's developer account through SMAPI and reports progress for the UI.
//
// A deploy builds the skill package, skips the rest if its hash matches the last successful deploy, then uploads and imports it, waits for every locale's interaction model to build, uploads a self-signed certificate if needed, enables the skill for testing and optionally smoke-tests it with the simulation API. The caller persists State between deploys; if it is lost, the skill is found again by name.
package deploy
