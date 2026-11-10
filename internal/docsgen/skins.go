package docsgen

import (
	"github.com/nightah/encore/internal/backend/kodi"
)

// SearchGuide renders the search guide, whose tables of skins come from the skin profiles Encore ships.
func SearchGuide(profiles []kodi.SkinProfile) ([]byte, error) {
	var own, native []kodi.SkinProfile

	for _, p := range profiles {
		if p.Native {
			native = append(native, p)
		} else {
			own = append(own, p)
		}
	}

	return render("search.md.tmpl", map[string]any{"Own": own, "Native": native})
}
