package match

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/fuzzy/phonetic/pt"
)

func TestNormalizePortuguese(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		// Accents, tildes, articles and ç, which is "ss".
		{"O Auto da Compadecida", "auto da compadecida"},
		{"A Grande Família", "grande familia"},
		{"Os Normais", "normais"},
		{"Uma Noite em 67", "noite em 67"},
		{"Umas e Outras", "e outras"},
		{"O", "o"},
		{"Sessão de Terapia", "sessao de terapia"},
		{"O Palhaço", "palhasso"},
		{"Ç", "ss"},
		// A decomposed ç (c and a combining cedilla), as NFD filenames store it.
		{"O Palhac\u0327o", "palhasso"},
		{"C\u0327", "ss"},
		{"Minha Mãe É uma Peça", "minha mae e uma pessa"},
		{"Copo d'Água", "copo dagua"},
		{"Chitãozinho & Xororó", "chitaozinho e xororo"},
		{"3%", "3 por 100"},
		{"três por cento", "3 por 100"},
		{"8½", "8 e meio"},
		// Units, the feminine forms and the other spellings.
		{"Dois Filhos de Francisco", "2 filhos de francisco"},
		{"Duas Caras", "2 caras"},
		{"Os Dez Mandamentos", "10 mandamentos"},
		{"Trem das Onze", "trem das 11"},
		{"catorze ou quatorze", "14 ou 14"},
		{"dezesseis ou dezasseis", "16 ou 16"},
		{"dezessete ou dezassete", "17 ou 17"},
		{"dezenove ou dezanove", "19 ou 19"},
		{"zero", "0"},
		// Tens and hundreds joined with "e", including the feminine hundreds.
		{"vinte e um", "21"},
		{"trinta e duas", "32"},
		{"cem", "100"},
		{"cento e um", "101"},
		{"cento e vinte e três", "123"},
		{"duzentos", "200"},
		{"duzentas e uma", "201"},
		{"quinhentos e cinquenta", "550"},
		{"novecentas", "900"},
		// Thousands and millions, which join hundreds with "e" too.
		{"mil", "1000"},
		{"mil e quinhentos", "1500"},
		{"dois mil e cem", "2100"},
		{"dois mil e vinte e quatro", "2024"},
		{"mil novecentos e oitenta e quatro", "1984"},
		{"cem mil", "100000"},
		{"um milhão", "1000000"},
		{"dois milhões e duzentos mil", "2200000"},
		// "um" and "uma" are only numbers inside one.
		{"Minha Mãe É 1 Peça", "minha mae e 1 pessa"},
		{"o poderoso chefão parte dois", "poderoso chefao 2"},
		// Ordinals, spelt out or written with digits and º or ª, and parts.
		{"O Poderoso Chefão: Parte II", "poderoso chefao 2"},
		{"primeiro", "1"},
		{"Segunda Chamada", "2 chamada"},
		{"terceiros", "3"},
		{"quartas", "4"},
		{"sétimo selo", "7 selo"},
		{"nona", "9"},
		{"décimo", "10"},
		{"vigésima", "20"},
		{"2ª temporada", "2 temporada"},
		{"1º episódio", "1 episodio"},
		{"3os", "3"},
		{"Cidade Invisível: Temporada 2", "cidade invisivel 2"},
		{"Capítulo 3", "3"},
		{"Volume 1", "1"},
		{"Tropa de Elite 2: O Inimigo Agora É Outro", "tropa de elite 2 o inimigo agora e outro"},
	} {
		assert.Equal(t, tc.want, Normalize("pt-BR", tc.in), tc.in)
	}
}

func TestRespellHook(t *testing.T) {
	l := english()
	assert.Equal(t, "facade", strings.Join(l.normalize("Façade", false), " "), "NFKD leaves c of ç")

	l.respell = strings.NewReplacer("ç", "ss", "%", " per cent ")
	assert.Equal(t, "fassade 3 per cent", strings.Join(l.normalize("Façade 3%", false), " "), "respell runs before NFKD")
	assert.Equal(t, "8 and a half", strings.Join(l.normalize("8½", false), " "), "½ is read before respelling")
}

func TestPortugueseLanguage(t *testing.T) {
	l := languageFor("pt-BR")
	assert.Equal(t, "pt", l.code)
	assert.Equal(t, []string{"SDJ", "SDD"}, l.phonetic("cidade"), "Portuguese keys with its own encoder")
	assert.Equal(t, pt.Encoder{}.Keys(nil, "xuxa"), l.phonetic("xuxa"))
	assert.True(t, l.stop["de"] && l.stop["nas"])
	assert.Equal(t, "e", l.and)
}

// TestPortugueseMatches checks that the Portuguese rules meet the spellings speech recognition writes for Brazilian titles end to end.
func TestPortugueseMatches(t *testing.T) {
	ix, err := New("pt-BR", Config{})
	require.NoError(t, err)

	t.Cleanup(func() { _ = ix.Close() })

	require.NoError(t, ix.Upsert(
		Doc{ID: "palhaco", Kind: "movie", Title: "O Palhaço"},
		Doc{ID: "xuxa", Kind: "artist", Title: "Xuxa"},
		Doc{ID: "timmaia", Kind: "artist", Title: "Tim Maia"},
		Doc{ID: "tres", Kind: "tvshow", Title: "3%"},
		Doc{ID: "tropa", Kind: "movie", Title: "Tropa de Elite"},
	))

	for heard, want := range map[string]string{
		"o palhasso":       "palhaco",
		"xuxa":             "xuxa",
		"chim maia":        "timmaia",
		"três por cento":   "tres",
		"tropa di eliti":   "tropa",
		"o palhaço":        "palhaco",
		"tim maia na sala": "",
	} {
		res, err := ix.Search(t.Context(), Query{Text: heard})
		require.NoError(t, err)

		if want == "" {
			assert.NotEqual(t, DecisionPlay, res.Decision, "%q", heard)

			continue
		}

		best, ok := res.Best()
		assert.True(t, ok, "%q", heard)
		assert.Equal(t, DecisionPlay, res.Decision, "%q: %s", heard, describe(res))
		assert.Equal(t, want, best.Doc.ID, "%q", heard)
	}
}
