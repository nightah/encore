package skillpkg

import "errors"

// ErrPrivacyUnanswered marks a build refused because the user has not answered the privacy and compliance questions.
var ErrPrivacyUnanswered = errors.New("skillpkg: privacy and compliance answers are required")
