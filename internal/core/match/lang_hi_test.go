package match

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"pgregory.net/rapid"
)

func TestNormalizeHindi(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		// Latin titles are compared as they are written.
		{"Dilwale Dulhania Le Jayenge", "dilwale dulhania le jayenge"},
		{"The Dark Knight", "dark knight"},
		// Devanagari is romanised the way film titles are, so it meets the Latin title.
		{"दंगल", "dangal"},
		{"शोले", "shole"},
		{"कभी खुशी कभी ग़म", "kabhi khushi kabhi gham"},
		{"तारक मेहता का उल्टा चश्मा", "tarak mehta ka ulta chashma"},
		// Schwa deletion: final, and medial between a vowel and a voiced consonant.
		{"धड़कन", "dhadkan"},
		{"दिलवाले", "dilvale"},
		{"अदरक", "adrak"},
		{"महाभारत", "mahabharat"},
		{"दुल्हनिया", "dulhaniya"},
		{"ज़िंदगी", "zindagi"},
		{"मित्र", "mitra"},
		{"न", "na"},
		// Nukta letters, ड़, nasals, glides and long vowels.
		{"क़यामत", "qayamat"},
		{"फ़िल्म", "film"},
		{"फिल्म", "philm"},
		{"मिर्ज़ापुर", "mirzapur"},
		{"चंपा", "champa"},
		{"सिंह", "singh"},
		{"दिलजीत दोसाँझ", "diljit dosanjh"},
		{"जाएंगे", "jayenge"},
		{"गए", "gaye"},
		{"धूम", "dhoom"},
		{"राजू", "raju"},
		{"ज्ञान", "gyan"},
		{"स्वदेश", "swadesh"},
		{"दुःख", "duhkh"},
		{"ॐ", "om"},
		{"में मैं हैं नहीं", "mein main hain nahin"},
		// English titles in Devanagari lose their article.
		{"द डार्क नाइट", "dark nait"},
		{"दी फ़ैमिली मैन", "faimili main"},
		// Letter names in a run are an initialism; one alone is a word.
		{"ए आर रहमान", "ar rahman"},
		{"मुन्ना भाई एम बी बी एस", "munna bhai mbbs"},
		{"दिल के पास", "dil ke pas"},
		// Mixed scripts, in one request and in one token.
		{"avengers एंडगेम", "avengers endagem"},
		{"abcदंगल", "abcdangal"},
		// Numbers: digits in either script, Hindi and English number words in either script.
		{"दंगल २०१६", "dangal 2016"},
		{"डॉन टू", "don 2"},
		{"थ्री इडियट्स", "3 idiyats"},
		{"धूम तीन", "dhoom 3"},
		{"Teen Patti", "3 patti"},
		{"Do Aur Do Paanch", "2 aur 2 5"},
		{"chaar sahibzaade", "4 sahibzaade"},
		{"twenty one", "21"},
		{"छः", "6"},
		{"पच्चीस", "25"},
		{"नब्बे", "90"},
		{"उन्नीस सौ बानवे", "1992"},
		{"स्कैम उन्नीस सौ बानवे", "skaim 1992"},
		{"दो सौ", "200"},
		{"एक हज़ार", "1000"},
		{"एक लाख बीस हज़ार", "120000"},
		{"दस lakh", "1000000"},
		{"ek crore", "10000000"},
		{"दो करोड़", "20000000"},
		// Ordinals, in words and in digits with a suffix.
		{"पहला", "1"},
		{"Pehla Nasha", "1 nasha"},
		{"तीसरी", "3"},
		{"छठा", "6"},
		{"बारहवीं", "12"},
		{"पाँचवाँ", "5"},
		{"सौवाँ", "100"},
		{"12वीं", "12"},
		{"12th", "12"},
		// Parts.
		{"बाहुबली भाग 2", "bahubli 2"},
		{"मिर्ज़ापुर सीज़न दो", "mirzapur 2"},
		{"Gangs of Wasseypur Part 2", "gangs of wasseypur 2"},
		{"भाग", "bhag"},
		// Halves.
		{"8½", "8 aur adha"},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			assert.Equal(t, tt.want, Normalize("hi-IN", tt.in))
		})
	}
}

func TestHindiRomanizer(t *testing.T) {
	plain, long := hindiRomanizer{}, hindiRomanizer{long: true}

	tests := []struct {
		in, plain, long string
	}{
		{"तीन", "tin", "teen"},
		{"चार", "char", "chaar"},
		{"धूम", "dhoom", "dhoom"},
		{"टू", "tu", "too"},
		{"dangal", "dangal", "dangal"},
		// Signs with nothing to attach to are kept as best they can be.
		{"ंक", "nk", "nk"},
		{"अा", "aa", "aaa"},
		{"्क़", "q", "q"},
		{"पिऍ", "piye", "piye"},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.plain, plain.romanize(tt.in), tt.in)
		assert.Equal(t, tt.long, long.romanize(tt.in), tt.in)
	}
}

func TestHindiStopWords(t *testing.T) {
	l := languages["hi"]

	assert.Equal(t, []string{"tarak", "mehta", "ulta", "chashma"}, l.queryTokens(&form{toks: l.normalize("तारक मेहता का उल्टा चश्मा", false)}), "postpositions are left out of retrieval")
	assert.Equal(t, []string{"ka", "ki"}, l.queryTokens(&form{toks: l.normalize("का की", false)}), "unless they are all there is")
}

// TestPropHindiRomanizeLatin checks that romanizing a Devanagari word always gives lowercase ASCII letters, so the romanised form normalises to itself.
func TestPropHindiRomanizeLatin(t *testing.T) {
	letters := []rune("अआइईउऊऋएऐओऔऑकखगघङचछजझञटठडढणतथदधनपफबभमयरलवशषसहािीुूृेैोौॉंँः़्")

	rapid.Check(t, func(t *rapid.T) {
		word := string(rapid.SliceOfN(rapid.SampledFrom(letters), 1, 12).Draw(t, "word"))

		for _, r := range []hindiRomanizer{{}, {long: true}} {
			got := r.romanize(word)
			if strings.ContainsFunc(got, func(c rune) bool { return c < 'a' || c > 'z' }) {
				t.Fatalf("romanize(%q) = %q", word, got)
			}
		}

		once := Normalize("hi", word)
		if twice := Normalize("hi", once); twice != once {
			t.Fatalf("Normalize(%q) = %q but Normalize(%q) = %q", word, once, once, twice)
		}
	})
}
