package docsgen

import (
	"github.com/nightah/encore/internal/backend/kodi"
)

// SkinSearchGuide renders the skin search guide, whose table of skins comes from the skin profiles Encore ships.
func SkinSearchGuide(profiles []kodi.SkinProfile) ([]byte, error) {
	var own, native []kodi.SkinProfile

	for _, p := range profiles {
		if p.Native {
			native = append(native, p)
		} else {
			own = append(own, p)
		}
	}

	return render("skin-search.md.tmpl", map[string]any{"Own": own, "Native": native})
}
