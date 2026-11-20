package match

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalizeSpanish(t *testing.T) {
	testCases := []struct {
		name     string
		have     string
		expected string
	}{
		// "veintiún" and "veintiuna" before a noun.
		{name: "ShouldNormalizeVeintiunGramos", have: "veintiún gramos", expected: "21 gramos"},
		{name: "ShouldNormalizeVeintiunaNoches", have: "Veintiuna noches", expected: "21 noches"},
		// Volumes are parts.
		{name: "ShouldNormalizeRecuerdosVolII", have: "Recuerdos, Vol. II", expected: "recuerdos 2"},
		{name: "ShouldNormalizeRecuerdosVolumenDos", have: "recuerdos volumen dos", expected: "recuerdos 2"},
		{name: "ShouldNormalizeElVolumen", have: "el volumen", expected: "volumen"},
		// Acronyms spelled out by letter name.
		{name: "ShouldNormalizeErreBeDe", have: "erre be de", expected: "rbd"},
		{name: "ShouldNormalizeAcronymAsWritten", have: "RBD", expected: "rbd"},
		{name: "ShouldNormalizePeErreCe", have: "pe erre ce", expected: "prc"},
		{name: "ShouldNormalizeErreECe", have: "erre e ce", expected: "rec"},
		{name: "ShouldNormalizeDeTeEmeEfe", have: "de te eme efe", expected: "dtmf"},
		{name: "ShouldNormalizeYeHacheEleCuEmeDeEleGe", have: "ye hache ele cu eme de ele ge", expected: "yhlqmdlg"},
		{name: "ShouldNormalizeConciertoDeErreBeDe", have: "concierto de erre be de", expected: "concierto de rbd"},
		{name: "ShouldNormalizeElDiscoDeTeCuEme", have: "el disco de te cu eme", expected: "disco de tqm"},
		{name: "ShouldNormalizeJotaBalvin", have: "jota balvin", expected: "jota balvin"},
		{name: "ShouldSpellDVD", have: "de ve de", expected: "dvd"},
		{name: "ShouldNotSpellPluralLetterName", have: "de ve des", expected: "de ve des"},
		{name: "ShouldNormalizeElConciertoEnDeVeDe", have: "el concierto en de ve de", expected: "concierto en dvd"},
		{name: "ShouldNormalizeElConciertoDeDeVeDe", have: "el concierto de de ve de", expected: "concierto de dvd"},
		// Common words that are also letter names stay words unless two other letter names spell with them.
		{name: "ShouldNormalizeTeDe", have: "te de", expected: "te de"},
		{name: "ShouldNormalizeEseDeAhi", have: "ese de ahí", expected: "ese de ahi"},
		{name: "ShouldNormalizeLaEle", have: "la ele", expected: "ele"},
		{name: "ShouldNormalizeCuEme", have: "cu eme", expected: "qm"},
		{name: "ShouldKeepLoneLetterNameAsWord", have: "ve", expected: "ve"},
		{name: "ShouldNormalizeVeACasa", have: "ve a casa", expected: "ve a casa"},
		{name: "ShouldNormalizeVeDeAhi", have: "ve de ahí", expected: "ve de ahi"},
		{name: "ShouldNormalizeVeTe", have: "ve te", expected: "ve te"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, Normalize("es-MX", tc.have))
		})
	}

	assert.Equal(t, "erre be de", Normalize("fr", "erre be de"), "only Spanish reads Spanish letter names")
}
