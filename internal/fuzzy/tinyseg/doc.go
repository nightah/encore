// Package tinyseg splits Japanese text into words without a dictionary. It is a Go port of Taku Kudo's TinySegmenter 0.2 (http://chasen.org/~taku/software/TinySegmenter/), whose boundary model scores each gap between characters from the characters, their classes (kanji numeral, kanji, hiragana, katakana, Latin, digit, other) and the decisions before it, with weights learnt from newspaper text. The weights are the original's, unchanged; the code is rewritten to pack the table keys into integers so segmenting allocates only the result.
//
// TinySegmenter is distributed under this licence, which covers the weights in weights.go:
//
//	Copyright (c) 2008, Taku Kudo
//
//	All rights reserved.
//
//	Redistribution and use in source and binary forms, with or without
//	modification, are permitted provided that the following conditions are met:
//
//	    * Redistributions of source code must retain the above copyright notice,
//	this list of conditions and the following disclaimer.
//	    * Redistributions in binary form must reproduce the above copyright
//	notice, this list of conditions and the following disclaimer in the
//	documentation and/or other materials provided with the distribution.
//	    * Neither the name of the <ORGANIZATION> nor the names of its
//	contributors may be used to endorse or promote products derived from this
//	software without specific prior written permission.
//
//	THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
//	"AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
//	LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
//	A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT OWNER OR
//	CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL,
//	EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT LIMITED TO,
//	PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE, DATA, OR
//	PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY THEORY OF
//	LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT (INCLUDING
//	NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE OF THIS
//	SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
package tinyseg
