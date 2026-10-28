// Package en registers the English phonetic encoder: Double Metaphone (Lawrence Philips, "The Double Metaphone Search Algorithm", C/C++ Users Journal, June 2000) with unbounded code length, plus an R-less key for non-rhotic speakers.
//
// The Double Metaphone rules are derived from github.com/PhonoGrams/double_metaphone v0.1.0 (Apache License 2.0, the same licence as Encore) and produce identical codes: this package's tests check the library's codes for every word of up to four letters (as a digest) and for 2004 generated words, malformed input included (testdata/library.txt).
package en
