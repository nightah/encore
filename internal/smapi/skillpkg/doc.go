// Package skillpkg builds the skill package Encore imports through SMAPI: a zip holding skill-package/skill.json, one interaction model per locale under skill-package/interactionModels/custom/, and the skill's icons under skill-package/assets/images/.
//
// Packages are deterministic: the same inputs give the same bytes and the same Hash, so callers can skip deploys that would change nothing.
package skillpkg
