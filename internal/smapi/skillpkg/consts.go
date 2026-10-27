package skillpkg

// Paths inside the package.
const (
	ManifestPath = "skill-package/skill.json"
	ModelDir     = "skill-package/interactionModels/custom/"
)

// Icon paths inside the package, and how the manifest refers to them.
const (
	IconDir      = "skill-package/assets/images/"
	smallIconURI = "file://assets/images/small-icon.png"
	largeIconURI = "file://assets/images/large-icon.png"
)

// DefaultCategory is the store category Encore declares.
//
// SMAPI's spec leaves category a free string, but Amazon only accepts its published list. None of the video entries fit a remote for a media centre on the user's own network: STREAMING_SERVICE suggests Encore provides the content and MOVIE_AND_TV_KNOWLEDGE_AND_TRIVIA is for trivia. SMART_HOME (skills that control devices in the home) is the closest. A development-stage skill is never listed, so this only matters if a user later submits the skill themselves.
const DefaultCategory = "SMART_HOME"

// sslCertificateType is what Encore declares for its endpoint. Alexa accepts both exact-name and wildcard certificates from a public authority under Wildcard, so nothing needs choosing.
const sslCertificateType = "Wildcard"

// Amazon's interaction model limits.
const (
	// MaxModelBytes is the largest interaction model Amazon accepts per locale.
	MaxModelBytes = 1_500_000
	// WarnModelBytes is where Encore starts warning that a model is close to the limit.
	WarnModelBytes = 1_200_000
	// MaxIntents is the most intents one model may have.
	MaxIntents = 250
	// MaxSlotValueLength is the longest slot value or synonym, in characters.
	MaxSlotValueLength = 140
)

const (
	summary     = "Control Kodi with your voice through your own Encore server."
	description = "A private skill served by encore, a self-hosted Alexa skill server for Kodi. It plays movies and TV shows from your Kodi library and controls playback and volume. It was deployed to your own Amazon developer account and works on the Echo devices registered to your account."
)

// askResources lets `ask deploy` (ASK CLI v2) deploy an exported package. The endpoint is Encore itself, so there is no code or infrastructure section.
const askResources = `{
  "askcliResourcesVersion": "2020-03-31",
  "profiles": {
    "default": {
      "skillMetadata": {
        "src": "./skill-package"
      }
    }
  }
}
`
