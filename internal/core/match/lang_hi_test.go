package match

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"pgregory.net/rapid"
)

func TestNormalizeHindi(t *testing.T) {
	testCases := []struct {
		name     string
		have     string
		expected string
	}{
		// Latin titles are compared as they are written.
		{name: "ShouldKeepLatinTitleAsWritten", have: "Dilwale Dulhania Le Jayenge", expected: "dilwale dulhania le jayenge"},
		{name: "ShouldDropArticleFromLatinTitle", have: "The Dark Knight", expected: "dark knight"},
		// Devanagari is romanised the way film titles are, so it meets the Latin title.
		{name: "ShouldReadDangal", have: "दंगल", expected: "dangal"},
		{name: "ShouldReadShole", have: "शोले", expected: "shole"},
		{name: "ShouldReadKabhiKhushiKabhiGham", have: "कभी खुशी कभी ग़म", expected: "kabhi khushi kabhi gham"},
		{name: "ShouldReadTarakMehtaKaUltaChashma", have: "तारक मेहता का उल्टा चश्मा", expected: "tarak mehta ka ulta chashma"},
		// Schwa deletion: final, and medial between a vowel and a voiced consonant.
		{name: "ShouldReadDhadkan", have: "धड़कन", expected: "dhadkan"},
		{name: "ShouldReadDilwale", have: "दिलवाले", expected: "dilwale"},
		{name: "ShouldReadGharwali", have: "घरवाली", expected: "gharwali"},
		{name: "ShouldReadKalwa", have: "कलवा", expected: "kalwa"},
		{name: "ShouldReadDivali", have: "दिवाली", expected: "divali"},
		{name: "ShouldReadAdrak", have: "अदरक", expected: "adrak"},
		{name: "ShouldReadMahabharat", have: "महाभारत", expected: "mahabharat"},
		{name: "ShouldReadDulhaniya", have: "दुल्हनिया", expected: "dulhaniya"},
		{name: "ShouldReadZindagi", have: "ज़िंदगी", expected: "zindagi"},
		{name: "ShouldReadMitra", have: "मित्र", expected: "mitra"},
		{name: "ShouldKeepSchwaOfLoneLetter", have: "न", expected: "na"},
		// Nukta letters, ड़, nasals, glides and long vowels.
		{name: "ShouldReadQayamat", have: "क़यामत", expected: "qayamat"},
		{name: "ShouldReadNuktaPhaAsF", have: "फ़िल्म", expected: "film"},
		{name: "ShouldReadPhaWithoutNuktaAsPh", have: "फिल्म", expected: "philm"},
		{name: "ShouldReadMirzapur", have: "मिर्ज़ापुर", expected: "mirzapur"},
		{name: "ShouldReadChampa", have: "चंपा", expected: "champa"},
		{name: "ShouldReadSingh", have: "सिंह", expected: "singh"},
		{name: "ShouldReadDiljitDosanjh", have: "दिलजीत दोसाँझ", expected: "diljit dosanjh"},
		{name: "ShouldReadVowelAfterVowelAsGlide", have: "जाएंगे", expected: "jayenge"},
		{name: "ShouldReadGaye", have: "गए", expected: "gaye"},
		{name: "ShouldReadDhoom", have: "धूम", expected: "dhoom"},
		{name: "ShouldReadRaju", have: "राजू", expected: "raju"},
		{name: "ShouldReadGyan", have: "ज्ञान", expected: "gyan"},
		{name: "ShouldReadSwadesh", have: "स्वदेश", expected: "swadesh"},
		{name: "ShouldReadDuhkh", have: "दुःख", expected: "duhkh"},
		{name: "ShouldReadOm", have: "ॐ", expected: "om"},
		{name: "ShouldReadMeinMainHainNahin", have: "में मैं हैं नहीं", expected: "mein main hain nahin"},
		// English titles in Devanagari lose their article.
		{name: "ShouldDropTheFromEnglishTitleInDevanagari", have: "द डार्क नाइट", expected: "dark nait"},
		{name: "ShouldDropDiFromEnglishTitleInDevanagari", have: "दी फ़ैमिली मैन", expected: "faimili main"},
		// Letter names in a run are an initialism; one alone is a word.
		{name: "ShouldJoinLetterNamesBeforeName", have: "ए आर रहमान", expected: "ar rahman"},
		{name: "ShouldJoinLetterNamesIntoInitialism", have: "मुन्ना भाई एम बी बी एस", expected: "munna bhai mbbs"},
		{name: "ShouldKeepLoneLetterNameAsWord", have: "दिल के पास", expected: "dil ke pas"},
		// Mixed scripts, in one request and in one token.
		{name: "ShouldReadMixedScriptsInOneRequest", have: "avengers एंडगेम", expected: "avengers endagem"},
		{name: "ShouldReadMixedScriptsInOneToken", have: "abcदंगल", expected: "abcdangal"},
		// Numbers: digits in either script, Hindi and English number words in either script.
		{name: "ShouldReadDevanagariDigits", have: "दंगल २०१६", expected: "dangal 2016"},
		{name: "ShouldReadEnglishNumberInDevanagari", have: "डॉन टू", expected: "don 2"},
		{name: "ShouldReadLeadingEnglishNumberInDevanagari", have: "थ्री इडियट्स", expected: "3 idiyats"},
		{name: "ShouldReadHindiNumberWord", have: "धूम तीन", expected: "dhoom 3"},
		{name: "ShouldReadHindiNumberWordInLatin", have: "Teen Patti", expected: "3 patti"},
		{name: "ShouldReadHindiNumberWordsInLatin", have: "Do Aur Do Paanch", expected: "2 aur 2 5"},
		{name: "ShouldReadChaarInLatin", have: "chaar sahibzaade", expected: "4 sahibzaade"},
		{name: "ShouldReadEnglishNumberWords", have: "twenty one", expected: "21"},
		{name: "ShouldReadSixWithVisarga", have: "छः", expected: "6"},
		{name: "ShouldReadTwentyFive", have: "पच्चीस", expected: "25"},
		{name: "ShouldReadNinety", have: "नब्बे", expected: "90"},
		{name: "ShouldReadYearInHundreds", have: "उन्नीस सौ बानवे", expected: "1992"},
		{name: "ShouldReadYearInHundredsAfterWord", have: "स्कैम उन्नीस सौ बानवे", expected: "skaim 1992"},
		{name: "ShouldReadHundreds", have: "दो सौ", expected: "200"},
		{name: "ShouldReadThousand", have: "एक हज़ार", expected: "1000"},
		{name: "ShouldReadLakhAndThousands", have: "एक लाख बीस हज़ार", expected: "120000"},
		{name: "ShouldReadLakhInLatin", have: "दस lakh", expected: "1000000"},
		{name: "ShouldReadCroreInLatin", have: "ek crore", expected: "10000000"},
		{name: "ShouldReadCrore", have: "दो करोड़", expected: "20000000"},
		{name: "ShouldReadCroreAndLakhs", have: "एक करोड़ बीस लाख", expected: "12000000"},
		{name: "ShouldReadCroreLakhsAndThousands", have: "दो करोड़ पाँच लाख दस हज़ार", expected: "20510000"},
		{name: "ShouldNotMultiplyLakhByCrore", have: "दो लाख करोड़", expected: "200000 10000000"},
		// Ordinals, in words and in digits with a suffix.
		{name: "ShouldReadFirst", have: "पहला", expected: "1"},
		{name: "ShouldReadFirstInLatin", have: "Pehla Nasha", expected: "1 nasha"},
		{name: "ShouldReadFeminineThird", have: "तीसरी", expected: "3"},
		{name: "ShouldReadSixth", have: "छठा", expected: "6"},
		{name: "ShouldReadTwelfth", have: "बारहवीं", expected: "12"},
		{name: "ShouldReadFifthWithNasalisation", have: "पाँचवाँ", expected: "5"},
		{name: "ShouldReadHundredth", have: "सौवाँ", expected: "100"},
		{name: "ShouldReadDigitOrdinalWithDevanagariSuffix", have: "12वीं", expected: "12"},
		{name: "ShouldReadDigitOrdinalWithEnglishSuffix", have: "12th", expected: "12"},
		// Parts.
		{name: "ShouldDropPartWord", have: "बाहुबली भाग 2", expected: "bahubli 2"},
		{name: "ShouldDropSeasonWord", have: "मिर्ज़ापुर सीज़न दो", expected: "mirzapur 2"},
		{name: "ShouldDropPartWordInLatin", have: "Gangs of Wasseypur Part 2", expected: "gangs of wasseypur 2"},
		{name: "ShouldKeepPartWordAlone", have: "भाग", expected: "bhag"},
		// Halves.
		{name: "ShouldReadHalf", have: "8½", expected: "8 aur adha"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, Normalize("hi-IN", tc.have))
		})
	}
}

func TestHindiRomanizer(t *testing.T) {
	plain, long := hindiRomanizer{}, hindiRomanizer{long: true}

	testCases := []struct {
		name  string
		have  string
		plain string
		long  string
	}{
		{name: "ShouldReadTeen", have: "तीन", plain: "tin", long: "teen"},
		{name: "ShouldReadChaar", have: "चार", plain: "char", long: "chaar"},
		{name: "ShouldReadDhoom", have: "धूम", plain: "dhoom", long: "dhoom"},
		{name: "ShouldReadToo", have: "टू", plain: "tu", long: "too"},
		{name: "ShouldKeepLatin", have: "dangal", plain: "dangal", long: "dangal"},
		// Signs with nothing to attach to are kept as best they can be.
		{name: "ShouldKeepAnusvaraWithNothingBefore", have: "ंक", plain: "nk", long: "nk"},
		{name: "ShouldKeepVowelSignAfterVowel", have: "अा", plain: "aa", long: "aaa"},
		{name: "ShouldKeepViramaWithNothingBefore", have: "्क़", plain: "q", long: "q"},
		{name: "ShouldReadCandraEAfterVowel", have: "पिऍ", plain: "piye", long: "piye"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.plain, plain.romanize(tc.have))
			assert.Equal(t, tc.long, long.romanize(tc.have))
		})
	}
}

func TestHindiStopWords(t *testing.T) {
	l := languages["hi"]

	testCases := []struct {
		name     string
		have     string
		expected []string
	}{
		{name: "ShouldLeavePostpositionsOutOfRetrieval", have: "तारक मेहता का उल्टा चश्मा", expected: []string{"tarak", "mehta", "ulta", "chashma"}},
		{name: "ShouldKeepPostpositionsWhenTheyAreAllThereIs", have: "का की", expected: []string{"ka", "ki"}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, l.queryTokens(&form{toks: l.normalize(tc.have, false)}))
		})
	}
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
