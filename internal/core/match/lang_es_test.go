package match

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalizeSpanish(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		// "veintiún" and "veintiuna" before a noun.
		{"veintiún gramos", "21 gramos"},
		{"Veintiuna noches", "21 noches"},
		// Volumes are parts.
		{"Recuerdos, Vol. II", "recuerdos 2"},
		{"recuerdos volumen dos", "recuerdos 2"},
		{"el volumen", "volumen"},
		// Acronyms spelled out by letter name.
		{"erre be de", "rbd"},
		{"RBD", "rbd"},
		{"pe erre ce", "prc"},
		{"erre e ce", "rec"},
		{"de te eme efe", "dtmf"},
		{"ye hache ele cu eme de ele ge", "yhlqmdlg"},
		{"concierto de erre be de", "concierto de rbd"},
		{"el disco de te cu eme", "disco de tqm"},
		{"jota balvin", "jota balvin"},
		// Common words that are also letter names stay words unless two other letter names spell with them.
		{"te de", "te de"},
		{"ese de ahí", "ese de ahi"},
		{"la ele", "ele"},
		{"cu eme", "qm"},
	} {
		assert.Equal(t, tc.want, Normalize("es-MX", tc.in), tc.in)
	}

	assert.Equal(t, "erre be de", Normalize("fr", "erre be de"), "only Spanish reads Spanish letter names")
}
