package i18n

import "embed"

// FS holds model.yaml, lang/ and locales/.
//
//go:embed model.yaml lang locales
var FS embed.FS
