package skillpkg

import _ "embed"

// The skill's icons, rendered from docs/brand/logo.svg by `make icons`. Amazon wants a 108×108 and a 512×512 PNG; the developer console and the Alexa app show them.
var (
	//go:embed assets/icon-108.png
	smallIcon []byte
	//go:embed assets/icon-512.png
	largeIcon []byte
)

func iconFiles() []File {
	return []File{
		{Name: IconDir + "small-icon.png", Data: smallIcon},
		{Name: IconDir + "large-icon.png", Data: largeIcon},
	}
}
