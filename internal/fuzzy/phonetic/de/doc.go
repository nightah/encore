// Package de registers the German phonetic encoder: Kölner Phonetik (Hans Joachim Postel, "Die Kölner Phonetik. Ein Verfahren zur Identifizierung von Personennamen auf der Grundlage der Gestaltanalyse", IBM-Nachrichten 19, 1969).
//
// The rules are derived from github.com/f1monkey/phonetic v1.4.2 (MIT License; its notice is kept in cologne.go) and produce identical codes: this package's tests check the library's codes for every word of up to four letters from a-z, ä, ö, ü and ß (as a digest) and for 1603 generated words, malformed input included (testdata/library.txt).
package de
