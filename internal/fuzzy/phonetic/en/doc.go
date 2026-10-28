// Package en registers the English phonetic encoder: Double Metaphone (Lawrence Philips, "The Double Metaphone Search Algorithm", C/C++ Users Journal, June 2000) with unbounded code length, plus an R-less key for non-rhotic speakers.
//
// The Double Metaphone rules are ported from github.com/PhonoGrams/double_metaphone v0.1.0 (Apache License 2.0, the same licence as Encore) and produce identical codes; the differential tests in this package check that against the library.
package en
