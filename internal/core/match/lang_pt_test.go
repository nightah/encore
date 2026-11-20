package match

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/fuzzy/phonetic/pt"
)

func TestNormalizePortuguese(t *testing.T) {
	testCases := []struct {
		name     string
		have     string
		expected string
	}{
		// Accents, tildes, articles and ç, which is "ss".
		{name: "ShouldNormalizeOAutoDaCompadecida", have: "O Auto da Compadecida", expected: "auto da compadecida"},
		{name: "ShouldNormalizeAGrandeFamilia", have: "A Grande Família", expected: "grande familia"},
		{name: "ShouldNormalizeOsNormais", have: "Os Normais", expected: "normais"},
		{name: "ShouldNormalizeUmaNoiteEm67", have: "Uma Noite em 67", expected: "noite em 67"},
		{name: "ShouldNormalizeUmasEOutras", have: "Umas e Outras", expected: "e outras"},
		{name: "ShouldKeepLoneArticle", have: "O", expected: "o"},
		{name: "ShouldNormalizeSessaoDeTerapia", have: "Sessão de Terapia", expected: "sessao de terapia"},
		{name: "ShouldNormalizeCedillaAsDoubleS", have: "O Palhaço", expected: "palhasso"},
		{name: "ShouldNormalizeCedillaAlone", have: "Ç", expected: "ss"},
		// A decomposed ç (c and a combining cedilla), as NFD filenames store it.
		{name: "ShouldNormalizeDecomposedCedilla", have: "O Palhac\u0327o", expected: "palhasso"},
		{name: "ShouldNormalizeDecomposedCedillaAlone", have: "C\u0327", expected: "ss"},
		{name: "ShouldNormalizeMinhaMaeEUmaPeca", have: "Minha Mãe É uma Peça", expected: "minha mae e uma pessa"},
		{name: "ShouldNormalizeCopoDAgua", have: "Copo d'Água", expected: "copo dagua"},
		{name: "ShouldNormalizeChitaozinhoAndXororo", have: "Chitãozinho & Xororó", expected: "chitaozinho e xororo"},
		{name: "ShouldNormalizePercentSign", have: "3%", expected: "3 por 100"},
		{name: "ShouldNormalizeTresPorCento", have: "três por cento", expected: "3 por 100"},
		{name: "ShouldNormalize8Half", have: "8½", expected: "8 e meio"},
		// Units, the feminine forms and the other spellings.
		{name: "ShouldNormalizeDoisFilhosDeFrancisco", have: "Dois Filhos de Francisco", expected: "2 filhos de francisco"},
		{name: "ShouldNormalizeDuasCaras", have: "Duas Caras", expected: "2 caras"},
		{name: "ShouldNormalizeOsDezMandamentos", have: "Os Dez Mandamentos", expected: "10 mandamentos"},
		{name: "ShouldNormalizeTremDasOnze", have: "Trem das Onze", expected: "trem das 11"},
		{name: "ShouldNormalizeCatorzeOuQuatorze", have: "catorze ou quatorze", expected: "14 ou 14"},
		{name: "ShouldNormalizeDezesseisOuDezasseis", have: "dezesseis ou dezasseis", expected: "16 ou 16"},
		{name: "ShouldNormalizeDezesseteOuDezassete", have: "dezessete ou dezassete", expected: "17 ou 17"},
		{name: "ShouldNormalizeDezenoveOuDezanove", have: "dezenove ou dezanove", expected: "19 ou 19"},
		{name: "ShouldNormalizeZero", have: "zero", expected: "0"},
		// Tens and hundreds joined with "e", including the feminine hundreds.
		{name: "ShouldNormalizeVinteEUm", have: "vinte e um", expected: "21"},
		{name: "ShouldNormalizeTrintaEDuas", have: "trinta e duas", expected: "32"},
		{name: "ShouldNormalizeCem", have: "cem", expected: "100"},
		{name: "ShouldNormalizeCentoEUm", have: "cento e um", expected: "101"},
		{name: "ShouldNormalizeCentoEVinteETres", have: "cento e vinte e três", expected: "123"},
		{name: "ShouldNormalizeDuzentos", have: "duzentos", expected: "200"},
		{name: "ShouldNormalizeDuzentasEUma", have: "duzentas e uma", expected: "201"},
		{name: "ShouldNormalizeQuinhentosECinquenta", have: "quinhentos e cinquenta", expected: "550"},
		{name: "ShouldNormalizeNovecentas", have: "novecentas", expected: "900"},
		// Thousands and millions, which join hundreds with "e" too.
		{name: "ShouldNormalizeMil", have: "mil", expected: "1000"},
		{name: "ShouldNormalizeMilEQuinhentos", have: "mil e quinhentos", expected: "1500"},
		{name: "ShouldNormalizeDoisMilECem", have: "dois mil e cem", expected: "2100"},
		{name: "ShouldNormalizeDoisMilEVinteEQuatro", have: "dois mil e vinte e quatro", expected: "2024"},
		{name: "ShouldNormalizeMilNovecentosEOitentaEQuatro", have: "mil novecentos e oitenta e quatro", expected: "1984"},
		{name: "ShouldNormalizeCemMil", have: "cem mil", expected: "100000"},
		{name: "ShouldNormalizeUmMilhao", have: "um milhão", expected: "1000000"},
		{name: "ShouldNormalizeDoisMilhoesEDuzentosMil", have: "dois milhões e duzentos mil", expected: "2200000"},
		// "um" and "uma" are only numbers inside one.
		{name: "ShouldNormalizeMinhaMaeE1PecaWithDigit", have: "Minha Mãe É 1 Peça", expected: "minha mae e 1 pessa"},
		{name: "ShouldNormalizeOPoderosoChefaoParteDoisSpoken", have: "o poderoso chefão parte dois", expected: "poderoso chefao 2"},
		// Ordinals, spelt out or written with digits and º or ª, and parts.
		{name: "ShouldNormalizeOPoderosoChefaoParteII", have: "O Poderoso Chefão: Parte II", expected: "poderoso chefao 2"},
		{name: "ShouldNormalizePrimeiro", have: "primeiro", expected: "1"},
		{name: "ShouldNormalizeSegundaChamada", have: "Segunda Chamada", expected: "2 chamada"},
		{name: "ShouldNormalizeTerceiros", have: "terceiros", expected: "3"},
		{name: "ShouldNormalizeQuartas", have: "quartas", expected: "4"},
		{name: "ShouldNormalizeSetimoSelo", have: "sétimo selo", expected: "7 selo"},
		{name: "ShouldNormalizeNona", have: "nona", expected: "9"},
		{name: "ShouldNormalizeDecimo", have: "décimo", expected: "10"},
		{name: "ShouldNormalizeVigesima", have: "vigésima", expected: "20"},
		{name: "ShouldNormalize2aTemporada", have: "2ª temporada", expected: "2 temporada"},
		{name: "ShouldNormalize1oEpisodio", have: "1º episódio", expected: "1 episodio"},
		{name: "ShouldNormalize3os", have: "3os", expected: "3"},
		{name: "ShouldNormalizeCidadeInvisivelTemporada2", have: "Cidade Invisível: Temporada 2", expected: "cidade invisivel 2"},
		{name: "ShouldNormalizeCapitulo3", have: "Capítulo 3", expected: "3"},
		{name: "ShouldNormalizeVolume1", have: "Volume 1", expected: "1"},
		{name: "ShouldNormalizeTropaDeElite2OInimigoAgoraEOutro", have: "Tropa de Elite 2: O Inimigo Agora É Outro", expected: "tropa de elite 2 o inimigo agora e outro"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, Normalize("pt-BR", tc.have))
		})
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

	testCases := []struct {
		name     string
		heard    string
		expected string
	}{
		{name: "ShouldFindCedillaHeardAsDoubleS", heard: "o palhasso", expected: "palhaco"},
		{name: "ShouldFindArtist", heard: "xuxa", expected: "xuxa"},
		{name: "ShouldFindAffricateHeardAsCh", heard: "chim maia", expected: "timmaia"},
		{name: "ShouldFindPercentSpokenAsWords", heard: "três por cento", expected: "tres"},
		{name: "ShouldFindEpentheticVowelsHeard", heard: "tropa di eliti", expected: "tropa"},
		{name: "ShouldFindTitleAsWritten", heard: "o palhaço", expected: "palhaco"},
		{name: "ShouldNotPlayTitleWithExtraWords", heard: "tim maia na sala"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := ix.Search(t.Context(), Query{Text: tc.heard})
			require.NoError(t, err)

			if tc.expected == "" {
				assert.NotEqual(t, DecisionPlay, res.Decision)

				return
			}

			best, ok := res.Best()
			assert.True(t, ok)
			assert.Equal(t, DecisionPlay, res.Decision, describe(res))
			assert.Equal(t, tc.expected, best.Doc.ID)
		})
	}
}
