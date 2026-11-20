package loanword

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"pgregory.net/rapid"
)

func TestNativeJapanese(t *testing.T) {
	testCases := []struct {
		name     string
		have     string
		expected []string
	}{
		{name: "ShouldWriteSuperfly", have: "Superfly", expected: []string{"スーパーフライ"}},
		{name: "ShouldWriteGlay", have: "GLAY", expected: []string{"グレイ"}},
		{name: "ShouldWriteQueen", have: "Queen", expected: []string{"クイーン"}},
		{name: "ShouldWriteTheBeatles", have: "The Beatles", expected: []string{"ザ ビートルズ", "ビートルズ"}},
		{name: "ShouldWriteStarWars", have: "Star Wars", expected: []string{"スター ウォーズ"}},
		{name: "ShouldWriteGhostInTheShellKeepingKanji", have: "GHOST IN THE SHELL / 攻殻機動隊", expected: []string{"ゴースト イン ザ シェル / 攻殻機動隊"}},
		{name: "ShouldWriteSeedAfterKanji", have: "機動戦士ガンダムSEED", expected: []string{"機動戦士ガンダムシード"}},
		{name: "ShouldWritePartAfterKatakanaKeepingNumeral", have: "ゴッドファーザー PART II", expected: []string{"ゴッドファーザー パート II"}},
		{name: "ShouldWriteFrozen", have: "Frozen", expected: []string{"フローズン"}},
		{name: "ShouldWriteHomeAlone", have: "Home Alone", expected: []string{"ホーム アローン"}},
		{name: "ShouldWriteHowever", have: "HOWEVER", expected: []string{"ハウエヴァー"}},
		{name: "ShouldWriteET", have: "E.T.", expected: []string{"イーティー"}},
		{name: "ShouldWriteAkb48", have: "AKB48", expected: []string{"エーケービー48"}},
		{name: "ShouldWriteOneOkRock", have: "ONE OK ROCK", expected: []string{"ワン オーケー ロック"}},
		{name: "ShouldWriteBackNumber", have: "back number", expected: []string{"バック ナンバー"}},
		{name: "ShouldWriteDreamsComeTrue", have: "DREAMS COME TRUE", expected: []string{"ドリームズ カム トルー"}},
		{name: "ShouldWriteMrsGreenApple", have: "Mrs. GREEN APPLE", expected: []string{"ミシズ. グリーン アップル"}},
		{name: "ShouldWriteCreepyNuts", have: "Creepy Nuts", expected: []string{"クリーピー ナッツ"}},
		{name: "ShouldWriteGetWild", have: "Get Wild", expected: []string{"ゲット ワイルド"}},
		{name: "ShouldWriteHarryPotter", have: "Harry Potter", expected: []string{"ハリー ポッター"}},
		{name: "ShouldWriteMissionImpossible", have: "Mission: Impossible", expected: []string{"ミッション: インポッシブル"}},
		{name: "ShouldWriteTheGodfather", have: "The Godfather", expected: []string{"ザ ゴッドファーザー", "ゴッドファーザー"}},
		{name: "ShouldWriteSquidGame", have: "Squid Game", expected: []string{"スクイッド ゲーム"}},
		{name: "ShouldWriteDune", have: "Dune", expected: []string{"デューン"}},
		{name: "ShouldWriteNewton", have: "Newton", expected: []string{"ニュートン"}},
		{name: "ShouldWriteSpiderManNoWayHome", have: "Spider-Man: No Way Home", expected: []string{"スパイダー-マン: ノー ウェイ ホーム"}},
		{name: "ShouldWriteTheWalkingDead", have: "The Walking Dead", expected: []string{"ザ ウォーキング デッド", "ウォーキング デッド"}},
		{name: "ShouldWriteBatmanBegins", have: "Batman Begins", expected: []string{"バットマン ビギンズ"}},
		{name: "ShouldWriteRockyV", have: "Rocky V", expected: []string{"ロッキー V"}},
		{name: "ShouldWriteStrangerThings", have: "Stranger Things", expected: []string{"ストレーンジャー シングズ"}},
		{name: "ShouldWriteCalmDown", have: "Calm Down", expected: []string{"カーム ダウン"}},
		{name: "ShouldWriteVikings", have: "Vikings", expected: []string{"ヴァイキングズ"}},
		{name: "ShouldWriteAWednesday", have: "A Wednesday!", expected: []string{"ア ウェンズデイ!"}},
		{name: "ShouldWriteAmelie", have: "Amélie", expected: []string{"アメリー"}},
		{name: "ShouldWriteRockNRoll", have: "Rock'n'Roll", expected: []string{"ロックンロル"}},
		{name: "ShouldWriteThe", have: "The", expected: []string{"ザ"}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, Native("ja-JP", tc.have))
		})
	}
}

func TestNativeHindi(t *testing.T) {
	testCases := []struct {
		name     string
		have     string
		expected []string
	}{
		{name: "ShouldWriteFrozen", have: "Frozen", expected: []string{"फ्रोजन", "फ्रोजेन"}},
		{name: "ShouldWriteMrIndia", have: "Mr. India", expected: []string{"मिस्टर. इंडिया", "म्र. इंदिआ"}},
		{name: "ShouldWriteDeewaar", have: "Deewaar", expected: []string{"दीवार", "डीवैआर"}},
		{name: "ShouldWriteZanjeer", have: "Zanjeer", expected: []string{"जैंजियर", "जंजीर"}},
		{name: "ShouldWriteYehJawaaniHaiDeewani", have: "Yeh Jawaani Hai Deewani", expected: []string{"ये जवानी है दीवनी", "ये जॉऐऐनी हे डीवॉनी"}},
		{name: "ShouldWriteTheBeatles", have: "The Beatles", expected: []string{"द बीटल्स", "थे बेअत्लेस"}},
		{name: "ShouldWriteKGFChapter1", have: "K.G.F: Chapter 1", expected: []string{"केजीएफ: चैप्टर 1", "केजीएफ: चप्तेर 1"}},
		{name: "ShouldWriteRrr", have: "RRR", expected: []string{"आरआरआर"}},
		{name: "ShouldWriteRDBurman", have: "R. D. Burman", expected: []string{"आरडी बरमैन", "आरडी बुर्मन"}},
		{name: "ShouldWriteMissionImpossible", have: "Mission: Impossible", expected: []string{"मिशन: इंपॉसिबल", "मिस्सिओन: इम्पोस्सिब्ले"}},
		{name: "ShouldWriteStrangerThings", have: "Stranger Things", expected: []string{"स्ट्रेंजर थिंग्स", "स्त्रंगेर थिंग्स"}},
		{name: "ShouldWriteChhotaBheem", have: "Chhota Bheem", expected: []string{"छोता भीम", "च्होटा ब्हीम"}},
		{name: "ShouldWriteKrrish", have: "Krrish", expected: []string{"क्रिश", "क्र्रिश"}},
		{name: "ShouldWritePushpa", have: "Pushpa", expected: []string{"पुश्पा", "पुश्पा"[:0] + "पुश्पा"}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := Native("hi-IN", tc.have)
			assert.Equal(t, dedupe(tc.expected), got)
		})
	}
}

func TestNativeArabic(t *testing.T) {
	testCases := []struct {
		name     string
		have     string
		expected []string
	}{
		{name: "ShouldWriteHomeAlone", have: "Home Alone", expected: []string{"هوم الون"}},
		{name: "ShouldWriteAvatar", have: "Avatar", expected: []string{"افاتار"}},
		{name: "ShouldWriteDune", have: "Dune", expected: []string{"ديون"}},
		{name: "ShouldWriteBeliever", have: "Believer", expected: []string{"بيليفر"}},
		{name: "ShouldWriteTheGodfather", have: "The Godfather", expected: []string{"ذا قودفاذر"}},
		{name: "ShouldWriteBraveheart", have: "Braveheart", expected: []string{"بريفهارت"}},
		{name: "ShouldWriteHello", have: "Hello", expected: []string{"هيلو"}},
		{name: "ShouldWriteVikings", have: "Vikings", expected: []string{"فايكينقز"}},
		{name: "ShouldWriteBts", have: "BTS", expected: []string{"بي تي اس"}},
		{name: "ShouldWriteET", have: "E.T.", expected: []string{"اي تي"}},
		{name: "ShouldWriteBatmanBegins", have: "Batman Begins", expected: []string{"باتمان بيقينز"}},
		{name: "ShouldWriteSuperfly", have: "Superfly", expected: []string{"سوبرفلاي"}},
		{name: "ShouldWriteOppenheimer", have: "Oppenheimer", expected: []string{"اوبنهايمر"}},
		{name: "ShouldWriteThings", have: "Things", expected: []string{"ثينقز"}},
		{name: "ShouldWritePink", have: "Pink", expected: []string{"بينك"}},
		{name: "ShouldWriteInception", have: "Inception", expected: []string{"انسيبشن"}},
		{name: "ShouldWriteIronMan", have: "Iron Man", expected: []string{"ايرن مان"}},
		{name: "ShouldWriteUp", have: "Up", expected: []string{"اب"}},
		{name: "ShouldWriteEatPrayLove", have: "Eat Pray Love", expected: []string{"ايت بري لف"}},
		{name: "ShouldWriteOceanSEleven", have: "Ocean's Eleven", expected: []string{"اوشنز اليفن"}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, Native("ar-SA", tc.have))
		})
	}
}

func dedupe(ss []string) []string {
	var out []string

	for _, s := range ss {
		if !contains(out, s) {
			out = append(out, s)
		}
	}

	return out
}

func TestNativeLeavesOtherTextAlone(t *testing.T) {
	t.Run("ShouldWriteNothing", func(t *testing.T) {
		testCases := []struct {
			name string
			lang string
			have string
		}{
			{name: "ShouldWriteNothingForLanguageWithoutWriter", lang: "en", have: "Superfly"},
			{name: "ShouldWriteNothingForGermanRegion", lang: "de-DE", have: "Superfly"},
			{name: "ShouldWriteNothingWithoutLatinLetters", lang: "ja", have: "千と千尋の神隠し"},
			{name: "ShouldWriteNothingForEmpty", lang: "ja"},
			{name: "ShouldWriteNothingForOnlyNumeral", lang: "ja", have: "II"},
			{name: "ShouldWriteNothingForArabicScript", lang: "ar", have: "تايتانك 2"},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				assert.Nil(t, Native(tc.lang, tc.have))
			})
		}
	})

	t.Run("ShouldReportSupportedLanguages", func(t *testing.T) {
		testCases := []struct {
			name     string
			have     string
			expected bool
		}{
			{name: "ShouldSupportJapaneseRegion", have: "ja-JP", expected: true},
			{name: "ShouldSupportHindiWithUnderscore", have: "hi_IN", expected: true},
			{name: "ShouldSupportUpperCaseArabic", have: "AR", expected: true},
			{name: "ShouldNotSupportFrench", have: "fr"},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				assert.Equal(t, tc.expected, Supports(tc.have))
			})
		}
	})
}

func TestNativeForms(t *testing.T) {
	testCases := []struct {
		name     string
		have     string
		expected []string
	}{
		{name: "ShouldAlsoDropLeadingThe", have: "The Beatles", expected: []string{"ザ ビートルズ", "ビートルズ"}},
		{name: "ShouldNotDropTheWhenNothingFollows", have: "The", expected: []string{"ザ"}},
		{name: "ShouldOnlyDropLeadingThe", have: "Here Comes the Sun", expected: []string{"ヒア カムズ ザ サン"}},
		{name: "ShouldNotTreatTheAfterOtherTextAsLeading", have: "「The Beatles」", expected: []string{"「ザ ビートルズ」"}},
		{name: "ShouldIgnoreSpacesBeforeLeadingThe", have: "  The Beatles", expected: []string{"ザ ビートルズ", "ビートルズ"}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, Native("ja", tc.have))
		})
	}

	assert.LessOrEqual(t, len(Native("hi", "The Fast and the Furious")), maxForms)
}

func TestInitialisms(t *testing.T) {
	t.Run("ShouldDetectUnsayableWords", func(t *testing.T) {
		testCases := []struct {
			name     string
			have     string
			expected bool
		}{
			{name: "ShouldSpellOutBTS", have: "BTS", expected: true},
			{name: "ShouldSpellOutAKB", have: "AKB", expected: true},
			{name: "ShouldSpellOutOK", have: "OK", expected: true},
			{name: "ShouldSpellOutTV", have: "TV", expected: true},
			{name: "ShouldSpellOutNHK", have: "NHK", expected: true},
			{name: "ShouldSpellOutUK", have: "UK", expected: true},
			{name: "ShouldSayGLAYAsWord", have: "GLAY"},
			{name: "ShouldSayZARDAsWord", have: "ZARD"},
			{name: "ShouldSaySEEDAsWord", have: "SEED"},
			{name: "ShouldSayONEAsWord", have: "ONE"},
			{name: "ShouldSayFORAsWord", have: "FOR"},
			{name: "ShouldSayGOAsWord", have: "GO"},
			{name: "ShouldSayDREAMSAsWord", have: "DREAMS"},
			{name: "ShouldSayALCHEMISTAsWord", have: "ALCHEMIST"},
			{name: "ShouldSaySMAPAsWord", have: "SMAP"},
			{name: "ShouldSayABBAAsWord", have: "ABBA"},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				assert.Equal(t, tc.expected, unsayable(fold(tc.have)))
			})
		}
	})

	t.Run("ShouldWriteLettersInKatakana", func(t *testing.T) {
		testCases := []struct {
			name     string
			have     string
			expected string
		}{
			{name: "ShouldWriteXJapan", have: "X JAPAN", expected: "エックス ジャパン"},
			{name: "ShouldWriteMalcolmX", have: "Malcolm X", expected: "マルコルム X"},
			{name: "ShouldWriteADay", have: "A Day", expected: "ア デイ"},
			{name: "ShouldWriteIRobot", have: "I Robot", expected: "アイ ローボット"},
			{name: "ShouldWriteU2", have: "U2", expected: "ユー2"},
			{name: "ShouldWriteJayZ", have: "Jay-Z", expected: "ジェイ-ゼット"},
			{name: "ShouldWriteSHE", have: "S.H.E", expected: "エスエイチイー"},
			{name: "ShouldWriteNeYo", have: "Ne-Yo", expected: "ニー-ヨー"},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				assert.Equal(t, []string{tc.expected}, Native("ja", tc.have))
			})
		}
	})
}

func TestEnglish(t *testing.T) {
	testCases := []struct {
		name     string
		have     string
		expected string
	}{
		{name: "ShouldPronounceGame", have: "game", expected: "g ei m"},
		{name: "ShouldPronounceStone", have: "stone", expected: "s t ou n"},
		{name: "ShouldPronounceRises", have: "rises", expected: "r ai s i z"},
		{name: "ShouldPronounceGames", have: "games", expected: "g ei m z"},
		{name: "ShouldPronounceBoxes", have: "boxes", expected: "b o k s i z"},
		{name: "ShouldPronounceCats", have: "cats", expected: "k ae t s"},
		{name: "ShouldPronounceNamed", have: "named", expected: "n ei m d"},
		{name: "ShouldPronounceWanted", have: "wanted", expected: "w o n t i d"},
		{name: "ShouldPronounceJumped", have: "jumped", expected: "j a m p t"},
		{name: "ShouldPronounceMaking", have: "making", expected: "m ei k i ng"},
		{name: "ShouldPronounceWalking", have: "walking", expected: "w oo k i ng"},
		{name: "ShouldPronounceRunning", have: "running", expected: "r a n i ng"},
		{name: "ShouldPronounceNation", have: "nation", expected: "n ae sh @ n"},
		{name: "ShouldPronounceVision", have: "vision", expected: "v i zh @ n"},
		{name: "ShouldPronounceMansion", have: "mansion", expected: "m ae n sh @ n"},
		{name: "ShouldPronouncePicture", have: "picture", expected: "p i k ch @ R"},
		{name: "ShouldPronouncePhone", have: "phone", expected: "f ou n"},
		{name: "ShouldPronounceGhost", have: "ghost", expected: "g ou s t"},
		{name: "ShouldPronounceNight", have: "night", expected: "n ai t"},
		{name: "ShouldPronounceEight", have: "eight", expected: "ei t"},
		{name: "ShouldPronounceThought", have: "thought", expected: "th oo t"},
		{name: "ShouldPronounceTough", have: "tough", expected: "t a f"},
		{name: "ShouldPronounceDough", have: "dough", expected: "d ou"},
		{name: "ShouldPronounceDaughter", have: "daughter", expected: "d oo t er R"},
		{name: "ShouldPronounceChorus", have: "chorus", expected: "ch ou r a s"},
		{name: "ShouldPronounceChrist", have: "christ", expected: "k r i s t"},
		{name: "ShouldPronounceSchool", have: "school", expected: "s k uu l"},
		{name: "ShouldPronounceKnife", have: "knife", expected: "n ai f"},
		{name: "ShouldPronounceWrite", have: "write", expected: "r ai t"},
		{name: "ShouldPronouncePsycho", have: "psycho", expected: "s i ch ou"},
		{name: "ShouldPronounceLamb", have: "lamb", expected: "l ae m"},
		{name: "ShouldPronounceAutumn", have: "autumn", expected: "oo t a m"},
		{name: "ShouldPronounceQuick", have: "quick", expected: "k w i Q k"},
		{name: "ShouldPronounceXenon", have: "xenon", expected: "z e n o n"},
		{name: "ShouldPronounceTaxi", have: "taxi", expected: "t ae k s ii"},
		{name: "ShouldPronounceBridge", have: "bridge", expected: "b r i j"},
		{name: "ShouldPronounceMagic", have: "magic", expected: "m ae j i k"},
		{name: "ShouldPronounceCity", have: "city", expected: "s i t ii"},
		{name: "ShouldPronounceAccept", have: "accept", expected: "ae k s e p t"},
		{name: "ShouldPronounceCycle", have: "cycle", expected: "s i k @ l"},
		{name: "ShouldPronounceMusic", have: "music", expected: "m y uu z i k"},
		{name: "ShouldPronounceSuper", have: "super", expected: "s uu p er R"},
		{name: "ShouldPronounceCute", have: "cute", expected: "k y uu t"},
		{name: "ShouldPronounceRude", have: "rude", expected: "r uu d"},
		{name: "ShouldPronounceCare", have: "care", expected: "k ea R"},
		{name: "ShouldPronounceFire", have: "fire", expected: "f ai @ R"},
		{name: "ShouldPronouncePure", have: "pure", expected: "p y uu @ R"},
		{name: "ShouldPronounceSure", have: "sure", expected: "sh oo R"},
		{name: "ShouldPronounceHour", have: "hour", expected: "h au @ R"},
		{name: "ShouldPronounceCourt", have: "court", expected: "k oo R t"},
		{name: "ShouldPronouncePoor", have: "poor", expected: "p oo R"},
		{name: "ShouldPronounceClear", have: "clear", expected: "k l ia R"},
		{name: "ShouldPronounceLearn", have: "learn", expected: "l er R n"},
		{name: "ShouldPronounceBeer", have: "beer", expected: "b ia R"},
		{name: "ShouldPronounceWord", have: "word", expected: "w er R d"},
		{name: "ShouldPronounceWarm", have: "warm", expected: "w oo R m"},
		{name: "ShouldPronounceQuarter", have: "quarter", expected: "k w oo R t er R"},
		{name: "ShouldPronounceBird", have: "bird", expected: "b er R d"},
		{name: "ShouldPronounceRain", have: "rain", expected: "r ei n"},
		{name: "ShouldPronounceLaw", have: "law", expected: "l oo"},
		{name: "ShouldPronounceHead", have: "head", expected: "h e d"},
		{name: "ShouldPronounceDeath", have: "death", expected: "d e th"},
		{name: "ShouldPronounceHeaven", have: "heaven", expected: "h e v @ n"},
		{name: "ShouldPronounceSea", have: "sea", expected: "s ii"},
		{name: "ShouldPronounceVein", have: "vein", expected: "v ei n"},
		{name: "ShouldPronounceHoney", have: "honey", expected: "h ou n ii"},
		{name: "ShouldPronounceThey", have: "they", expected: "dh ei"},
		{name: "ShouldPronounceNew", have: "new", expected: "n y uu"},
		{name: "ShouldPronounceBlue", have: "blue", expected: "b l uu"},
		{name: "ShouldPronouncePie", have: "pie", expected: "p ai"},
		{name: "ShouldPronounceMovie", have: "movie", expected: "m ou v ii"},
		{name: "ShouldPronounceBoat", have: "boat", expected: "b ou t"},
		{name: "ShouldPronounceToe", have: "toe", expected: "t ou"},
		{name: "ShouldPronounceBoy", have: "boy", expected: "b oi"},
		{name: "ShouldPronounceBook", have: "book", expected: "b u k"},
		{name: "ShouldPronounceMoon", have: "moon", expected: "m uu n"},
		{name: "ShouldPronounceHouse", have: "house", expected: "h au s"},
		{name: "ShouldPronounceShow", have: "show", expected: "sh ou"},
		{name: "ShouldPronounceNow", have: "now", expected: "n au"},
		{name: "ShouldPronounceKnown", have: "known", expected: "n ou n"},
		{name: "ShouldPronounceDown", have: "down", expected: "d au n"},
		{name: "ShouldPronounceSuit", have: "suit", expected: "s uu t"},
		{name: "ShouldPronounceGuitar", have: "guitar", expected: "g i t aa R"},
		{name: "ShouldPronounceBuy", have: "buy", expected: "b ai"},
		{name: "ShouldPronounceTalk", have: "talk", expected: "t oo k"},
		{name: "ShouldPronounceBall", have: "ball", expected: "b oo l"},
		{name: "ShouldPronounceSalt", have: "salt", expected: "s oo l t"},
		{name: "ShouldPronounceWant", have: "want", expected: "w o n t"},
		{name: "ShouldPronouncePizza", have: "pizza", expected: "p i z @"},
		{name: "ShouldPronounceSeven", have: "seven", expected: "s e v @ n"},
		{name: "ShouldPronounceKind", have: "kind", expected: "k ai n d"},
		{name: "ShouldPronounceWild", have: "wild", expected: "w ai l d"},
		{name: "ShouldPronounceBold", have: "bold", expected: "b ou l d"},
		{name: "ShouldPronounceHost", have: "host", expected: "h ou s t"},
		{name: "ShouldPronounceLost", have: "lost", expected: "l o s t"},
		{name: "ShouldPronounceHotel", have: "hotel", expected: "h ou t e l"},
		{name: "ShouldPronouncePush", have: "push", expected: "p u sh"},
		{name: "ShouldPronounceCup", have: "cup", expected: "k a p"},
		{name: "ShouldPronounceUnit", have: "unit", expected: "y uu n i t"},
		{name: "ShouldPronounceMy", have: "my", expected: "m ai"},
		{name: "ShouldPronounceHappy", have: "happy", expected: "h ae Q p ii"},
		{name: "ShouldPronounceGym", have: "gym", expected: "g i m"},
		{name: "ShouldPronounceSkyfall", have: "skyfall", expected: "s k i f oo l"},
		{name: "ShouldPronounceTiger", have: "tiger", expected: "t ai g er R"},
		{name: "ShouldPronounceLove", have: "love", expected: "l a v"},
		{name: "ShouldPronounceGlove", have: "glove", expected: "g l a v"},
		{name: "ShouldPronounceStove", have: "stove", expected: "s t ou v"},
		{name: "ShouldPronounceYes", have: "yes", expected: "y e s"},
		{name: "ShouldPronounceYellow", have: "yellow", expected: "y e l ou"},
		{name: "ShouldPronounceHello", have: "hello", expected: "h e l ou"},
		{name: "ShouldPronounceCarry", have: "carry", expected: "k ae r ii"},
		{name: "ShouldPronounceFamous", have: "famous", expected: "f ae m @ s"},
		{name: "ShouldPronounceGodfather", have: "godfather", expected: "g o d f aa dh @ R"},
		{name: "ShouldPronounceBraveheart", have: "braveheart", expected: "b r ei v h aa R t"},
		{name: "ShouldPronounceHe", have: "he", expected: "h ii"},
		{name: "ShouldPronounceWatch", have: "watch", expected: "w o ch"},
		{name: "ShouldPronounceWhite", have: "white", expected: "w ai t"},
		{name: "ShouldPronouncePage", have: "page", expected: "p ei j"},
		{name: "ShouldPronounceOrange", have: "orange", expected: "o r ae n j"},
		{name: "ShouldPronounceRosa", have: "rosa", expected: "r ou z @"},
		{name: "ShouldPronounceEarth", have: "earth", expected: "er R th"},
		{name: "ShouldPronounceHearts", have: "hearts", expected: "h aa R t s"},
		{name: "ShouldPronounceHearty", have: "hearty", expected: "h aa R t ii"},
		{name: "ShouldPronounceWorth", have: "worth", expected: "w er R th"},
		{name: "ShouldPronounceFort", have: "fort", expected: "f oo R t"},
		{name: "ShouldPronounceGrey", have: "grey", expected: "g r ei"},
		{name: "ShouldPronounceTheme", have: "theme", expected: "th ii m"},
		{name: "ShouldPronounceMenu", have: "menu", expected: "m e n y uu"},
		{name: "ShouldPronounceAh", have: "ah", expected: "ae"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, strings.Join(english(tc.have), " "))
		})
	}
}

func TestRomanisedScore(t *testing.T) {
	testCases := []struct {
		name     string
		have     string
		expected int
	}{
		{name: "ShouldScoreDeewaarAsRomanised", have: "deewaar", expected: 1},
		{name: "ShouldScoreKhabarAsRomanised", have: "khabar", expected: 1},
		{name: "ShouldScoreJawaniAsRomanised", have: "jawani", expected: 1},
		{name: "ShouldScoreChhotaAsRomanised", have: "chhota", expected: 1},
		{name: "ShouldScoreKaunAsNeutral", have: "kaun"},
		{name: "ShouldScoreDuneAsEnglish", have: "dune", expected: -1},
		{name: "ShouldScorePictureAsEnglish", have: "picture", expected: -2},
		{name: "ShouldScoreNightAsEnglish", have: "night", expected: -1},
		{name: "ShouldScoreRockAsEnglish", have: "rock", expected: -2},
		{name: "ShouldScoreTheAsEnglish", have: "the", expected: -2},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, romanisedScore(tc.have))
		})
	}
}

func TestWriters(t *testing.T) {
	assert.Equal(t, "पिंक", devanagari(english("pink")), "ng before k is the anusvara alone")
	assert.Equal(t, "अ", romanHindi("a1"), "a character that is no letter is skipped")
	var b strings.Builder
	writeVowel(&b, "q", "a", "", func() {})
	assert.Equal(t, "ア", b.String(), "a consonant without a row writes the vowel alone")

	r := &reader{w: "ae"}
	r.emit("a")
	r.u(false)
	assert.Equal(t, []phone{"a", "uu"}, r.out, "a long u after a vowel has no glide")

	t.Run("ShouldWritePalatalSyllables", func(t *testing.T) {
		testCases := []struct {
			name      string
			consonant phone
			vowel     phone
			expected  string
		}{
			{name: "ShouldWriteKyu", consonant: "k", vowel: "uu", expected: "キュ"},
			{name: "ShouldWriteKi", consonant: "k", vowel: "i", expected: "キ"},
			{name: "ShouldWriteShu", consonant: "s", vowel: "uu", expected: "シュ"},
			{name: "ShouldWriteJo", consonant: "j", vowel: "o", expected: "ジョ"},
			{name: "ShouldWriteTyu", consonant: "t", vowel: "uu", expected: "テュ"},
			{name: "ShouldWriteGlideAloneForConsonantWithoutRow", consonant: "q", vowel: "a", expected: "ヤ"},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				assert.Equal(t, tc.expected, palatal(tc.consonant, tc.vowel))
			})
		}
	})

	t.Run("ShouldWriteConsonantsAlone", func(t *testing.T) {
		testCases := []struct {
			name     string
			have     phone
			expected string
		}{
			{name: "ShouldWriteChi", have: "ch", expected: "チ"},
			{name: "ShouldWriteJi", have: "j", expected: "ジ"},
			{name: "ShouldWriteNothingForH", have: "h"},
			{name: "ShouldWriteNothingForGlottalStop", have: "q"},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				assert.Equal(t, tc.expected, alone(tc.have))
			})
		}
	})

	t.Run("ShouldWriteKatakana", func(t *testing.T) {
		testCases := []struct {
			name     string
			have     []phone
			expected string
		}{
			{name: "ShouldWriteTuu", have: []phone{"t", "uu"}, expected: "トゥー"},
			{name: "ShouldWriteCute", have: english("cute"), expected: "キュート"},
			{name: "ShouldWriteDiphthongAu", have: []phone{"au"}, expected: "アウ"},
			{name: "ShouldKeepSecondVowelOfDiphthongAfterGlide", have: []phone{"k", "y", "ai"}, expected: "キャイ"},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				assert.Equal(t, tc.expected, katakana(tc.have))
			})
		}
	})

	t.Run("ShouldFoldLatinLetters", func(t *testing.T) {
		testCases := []struct {
			name     string
			have     string
			expected string
		}{
			{name: "ShouldFoldEszett", have: "Straße", expected: "strasse"},
			{name: "ShouldFoldAeLigature", have: "Æon", expected: "aeon"},
			{name: "ShouldFoldOeLigature", have: "Œuvre", expected: "oeuvre"},
			{name: "ShouldFoldSlashedO", have: "Øresund", expected: "oresund"},
			{name: "ShouldFoldStrokedL", have: "Łódź", expected: "lodz"},
			{name: "ShouldFoldStrokedD", have: "Đorđe", expected: "dorde"},
			{name: "ShouldFoldThorn", have: "Þór", expected: "thor"},
			{name: "ShouldFoldDotlessI", have: "ıstanbul", expected: "istanbul"},
			{name: "ShouldFoldEth", have: "Ðe", expected: "de"},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				assert.Equal(t, tc.expected, fold(tc.have))
			})
		}
	})
}

// TestPropNative checks that Native never panics, writes no Latin letters for a language it supports and keeps whatever isn't a Latin word.
func TestPropNative(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		s := rapid.StringMatching(`[A-Za-zéüß'.\- 0-9千ア]{0,30}`).Draw(t, "s")
		lang := rapid.SampledFrom([]string{"ja", "hi", "ar"}).Draw(t, "lang")

		for _, f := range Native(lang, s) {
			for _, r := range f {
				if isLatin(r) && !strings.ContainsRune("IVX", r) {
					t.Fatalf("Native(%q, %q) left a Latin letter in %q", lang, s, f)
				}
			}

			if strings.Contains(s, "千") && !strings.Contains(f, "千") {
				t.Fatalf("Native(%q, %q) = %q lost the kanji", lang, s, f)
			}
		}
	})
}
