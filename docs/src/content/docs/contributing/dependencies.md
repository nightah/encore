---
title: Dependencies and benchmarks
description: Why Encore implements its search index, edit distances, TOTP and phonetic matching itself, how each replacement was checked, and what it measured.
---

Encore keeps its dependencies few, because every library is code that ships in the binary, needs watching for security fixes and has to keep fitting how Encore uses it. In October 2026 every library was reviewed for recency, maintenance and fit. Five were replaced with code in Encore's own tree and two were kept. This page explains each decision, how the replacement was shown to behave the same, and what was measured.

The code that compares Encore's implementations with the libraries is public, in the [`benchmarks/`](https://github.com/nightah/encore/tree/main/benchmarks) directory: a Go module of its own, so the replaced libraries never return to Encore's `go.mod`. Its [README](https://github.com/nightah/encore/tree/main/benchmarks#readme) has the exact commands, and the raw output of every run published here is kept next to it.

## At a glance

| Library | What Encore used it for | Now | Why it went |
| --- | --- | --- | --- |
| [bleve](https://github.com/blevesearch/bleve) | Finding candidate titles for a voice request | An in-memory inverted index in `internal/core/match` | 27 modules and about 8 MB of binary for one query shape; the in-tree index retrieves 7 to 10 times faster with a fraction of the allocations |
| [go-edlib](https://github.com/hbollon/go-edlib) | Jaro-Winkler and optimal string alignment distance when scoring titles | Two functions in `internal/core/match` | Two functions from a general library, on the review's maintenance watch list; the in-tree ones give identical results without allocating |
| [pquerna/otp](https://github.com/pquerna/otp) | TOTP for the local admin account | RFC 6238 on the standard library, in `internal/auth` | One fixed set of options out of a configurable library, on the review's maintenance watch list, which also linked in a barcode library Encore never used |
| [PhonoGrams/double_metaphone](https://github.com/PhonoGrams/double_metaphone) | English sound-alike keys | `internal/fuzzy/phonetic/en`, ported with identical output | A small library on the review's maintenance watch list; the port is about five times faster, with one allocation per word instead of eleven |
| [f1monkey/phonetic](https://github.com/f1monkey/phonetic) | German sound-alike keys (Kölner Phonetik) | `internal/fuzzy/phonetic/de`, ported with identical output | Pulled in `golang.org/x/exp` for one helper; the port is about four times faster and doesn't allocate |
| [miekg/dns](https://github.com/miekg/dns) | Finding Kodi instances with mDNS | Kept | Its successor is larger and fails on names Kodi instances can have |
| [gowebdav](https://github.com/studio-b12/gowebdav) | WebDAV backups | Kept | The alternative sends uploads Nextcloud can store as empty files |

`golang.org/x/exp` left with f1monkey/phonetic, which was the only thing that needed it. Encore also gained French and Spanish phonetic encoders that no library offered.

## How the replacements were checked

Each replacement followed the same steps, so that what Encore does for its users didn't change:

1. **Pin the current behaviour.** Before touching a library, tests recorded what Encore did with it: the RFC test vectors and every edge of the TOTP skew window, reference values for the edit distances, the candidates the search index returned for every case of the speech-recognition test corpus.
2. **Compare side by side.** The in-tree code was written and run against the library on the same inputs: every short word or string, rapid-generated text (arbitrary Unicode, invalid UTF-8, very long input), and the test corpora. It shipped only once the results were identical, or, for the search index, the same rankings.
3. **Break it on purpose.** A test that can't fail proves nothing, so the checks were shown to catch a real difference: for the phonetic ports, changing one rule in each (a Kölner Phonetik context, a Double Metaphone code) makes them fail.
4. **Keep checking.** When a library was removed, its output was captured as fixed expectations in Encore's tests, and the side-by-side comparisons moved to `benchmarks/`, where CI runs them on every change.

## Search index: bleve

When Alexa hears a title, Encore first narrows the library to several hundred candidates and then scores each one carefully. bleve, a full-text search engine, found those candidates: fuzzy word matches, sound-alike keys and, when those found little, character trigrams, ranked by TF-IDF.

**Why it was replaced.** Encore used one query shape of a general search engine, and paid for all of it: bleve and its storage formats were 27 modules and about 8 MB of a 38.6 MB binary. Search allocated tens of thousands of objects per request at large library sizes, and bleve's tokenizer split titles in scripts written without spaces (Chinese, Japanese kana) into single characters and dropped Thai, so a near miss of such a title found nothing.

**What replaced it.** An in-memory inverted index over the same words, phonetic keys and trigrams, running the same query and scoring it the same way, coordination factors included, so it ranks candidates as bleve did. Words in scripts without spaces stay whole. Deleted titles are dropped from the index by a compaction that runs in the background, so a large library change never blocks voice requests.

**How it was checked.** Differential tests compared the two retrievers' full rankings on the corpus libraries with every case and title as a query, on a 10,000-title synthetic library, and on generated libraries (accented, Cyrillic, Greek, Hebrew and Arabic words, stop words, numbers, very short and very long titles, duplicates, kind filters) after runs of updates and deletes. Before bleve was removed, the rankings for every corpus case were pinned as fixed expectations. The index is also checked against a reference scorer that scores every title straight from the query's definition, and a state-machine test interleaves updates, deletes and every step of a compaction. In `benchmarks/`, bleve as Encore last shipped it (commit `96d0dfaa`) runs through the same matcher, and the parity tests check that it returns the same candidates and the same results.

### Results

Bleve against the in-tree index on 28 October 2026, at commit `f8b8a770` against `96d0dfaa`, with the candidate pool of 200 that both used then. Go 1.27, 8 interleaved rounds, `-benchtime 3496x`, compared with benchstat (every difference p=0.000). The machine was also running other work (load average 4.8 to 12.1).

| Benchmark | bleve | In-tree | Change |
| --- | --- | --- | --- |
| Search, 100k titles | 2195 µs | 879 µs | −60% |
| Search, scale library, 100k | 2060 µs | 793 µs | −62% |
| Search, scale library, 500k | 6101 µs | 1648 µs | −73% |
| Candidates only, 100k | 1481 µs | 162 µs | −89% |
| Candidates only, 500k | 5763 µs | 609 µs | −89% |
| Search p99, 100k titles | 7.45 ms | 2.59 ms | −65% |
| Search p99, scale library, 500k | 36.25 ms | 14.88 ms | −59% |
| Candidates p99, 500k | 37.87 ms | 3.61 ms | −90% |
| Memory per search, scale library, 500k | 3689 KiB | 206 KiB | −94% |
| Allocations per search, scale library, 500k | 32,626 | 427 | −99% |

The same comparison rerun from `benchmarks/` on 29 October 2026 at commit `1473776f`, with today's default pool of 800 candidates. Go 1.27.3 on an AMD Ryzen 9 9950X3D, 8 interleaved rounds, `-benchtime 1000x` (`3x` for building), compared with benchstat (every difference p=0.000). The machine was also running other work (load average 3.6 to 6.4). Whole searches take longer than in the first table because the scorer now gets 800 candidates instead of 200; retrieval alone is comparable.

| Benchmark | bleve | In-tree | Change |
| --- | --- | --- | --- |
| Search, 100k titles | 4.11 ms | 2.53 ms | −38% |
| Search, scale library, 100k | 3.89 ms | 2.43 ms | −38% |
| Search, scale library, 500k | 7.93 ms | 3.24 ms | −59% |
| Candidates only, 100k | 1529 µs | 211 µs | −86% |
| Candidates only, 500k | 5437 µs | 601 µs | −89% |
| Search p99, 100k titles | 12.5 ms | 8.0 ms | −36% |
| Search p99, scale library, 500k | 43.1 ms | 14.1 ms | −67% |
| Candidates p99, 500k | 38.3 ms | 3.0 ms | −92% |
| Memory per search, scale library, 500k | 4190 KiB | 415 KiB | −90% |
| Allocations per search, scale library, 500k | 35,457 | 1,434 | −96% |
| Building an index of 100k titles | 2.15 s | 0.55 s | −74% |
| Live heap after building it | 138.5 MiB | 128.7 MiB | −7% |

The *scale library* is synthetic titles whose common words follow a Zipf distribution, as real libraries' do, with the speech-recognition test corpus inside it; it is queried with 700 synthetic mishearings and the corpus's 174 cases. *Search* is the whole request: retrieval, scoring and the decision. p99 is the 99th percentile of individual requests.

### Compaction

Deleted titles leave tombstones in the index. Once they are a quarter of it, a compaction rebuilds the index without them. The first version ran it during the delete, under the lock every voice request needs; it now runs in the background and swaps the result in at the end. Measured with `BenchmarkInvertedCompact` in `internal/core/match`, which deletes a quarter of a scale library while a reader keeps retrieving candidates, at commit `bed8e145` against its parent, 8 interleaved rounds:

| Library | Longest wait for a request, before | After |
| --- | --- | --- |
| 100,000 titles | 116 ms | 1.6 ms |
| 500,000 titles | 667 ms | 3.9 ms |

The background compaction costs memory while it runs: the peak heap during a quarter-library delete went from 105 to 108 MiB at 100,000 titles and from 477 to 486 MiB at 500,000 (+2%), and the process's peak resident memory was slightly lower (966 to 945 MiB).

### Candidate pool

The scorer used to get 200 candidates. At 100,000 titles and more, common words could fill that pool with other titles before the right one, so it was measured at 200, 400, 800 and 1600 with the scale harness (`TestRetrieverScale`, commit `18b3b66f`), counting agreement with scoring every title exhaustively over 874 queries and the corpus cases decided correctly:

| Library | Pool | Agreement | Corpus cases right (of 174) | Search p50 | Search p99 |
| --- | --- | --- | --- | --- | --- |
| 10k | 200 | 99.2% | 165 | 0.50 ms | 2.0 ms |
| 10k | 800 | 99.5% | 165 | 1.05 ms | 7.4 ms |
| 100k | 200 | 96.8% | 160 | 0.61 ms | 2.6 ms |
| 100k | 800 | 98.5% | 161 | 2.42 ms | 8.6 ms |
| 100k | 1600 | 98.7% | 161 | 4.49 ms | 17.0 ms |
| 500k | 200 | 92.6% | 153 | 0.80 ms | 13.9 ms |
| 500k | 800 | 95.2% | 155 | 2.58 ms | 16.0 ms |
| 500k | 1600 | 97.0% | 155 | 4.75 ms | 17.9 ms |

800 is now the default: it decides one more corpus case at 100k and two more at 500k than 200 does, with no wrong plays, while 1600 decides no more and doubles the time again. A search at 800 still takes a few milliseconds, against the hundreds Alexa's round trip takes.

## Edit distances: go-edlib

Scoring compares the request with each candidate title by Jaro-Winkler similarity and optimal string alignment distance. Encore used those two functions of go-edlib, a general library of string distances.

**Why it was replaced.** Two short functions didn't justify a dependency on a library the review had on its maintenance watch list, and go-edlib allocated on every call, several times per candidate per request.

**What replaced it.** Rune-based implementations in `internal/core/match/similarity.go` that decode strings of up to 64 characters into stack buffers, so scoring doesn't allocate.

**How it was checked.** A property test compared them bit for bit with go-edlib 1.7.0 across random Unicode, invalid UTF-8 and long strings, and reference values captured from go-edlib stay in Encore's tests. `benchmarks/editdistance` keeps the comparison, adding every pair of strings of up to three characters over an alphabet with an accented letter and an emoji.

**Results.** From `benchmarks/editdistance` on 29 October 2026, in the same run as the search results above, over title pairs such as "seinfield" and "seinfeld" or "the office" and "office the":

| Function | go-edlib | In-tree | Change |
| --- | --- | --- | --- |
| Jaro-Winkler | 125.8 ns, 312 B, 2 allocations | 58.4 ns, no allocations | −54% |
| Optimal string alignment | 505.5 ns, 476 B, 4 allocations | 292.1 ns, no allocations | −42% |

When it was replaced (commit `6a81dce1`), scoring alone took 27% less time with 92% fewer allocations, and a search over 100,000 titles allocated 39% fewer objects.

## TOTP: pquerna/otp

The local admin account can require a code from an authenticator app (TOTP). Encore used pquerna/otp to generate secrets, enrolment URLs and codes.

**Why it was replaced.** Encore uses one fixed set of options (SHA1, six digits, 30-second steps, one step of clock skew), which takes about 100 lines on top of Go's standard library, and pquerna/otp linked a barcode library into the binary that Encore never called.

**What replaced it.** RFC 6238 over RFC 4226 in `internal/auth/totp.go`, using `crypto/hmac` and `crypto/sha1`, with codes compared in constant time. Secrets already enrolled keep working.

**How it was checked.** Before the swap, tests pinned the RFC 6238 and RFC 4226 test vectors, the skew window at both edges of a step, secrets in every encoding authenticator apps produce, code input handling and the exact enrolment URL. `benchmarks/totp` keeps the comparison with pquerna/otp: the RFC vectors for both, and the same codes, the same accepted and rejected codes, and the same secret and URL from the same random bytes, over generated secrets and times.

**Results.** From `benchmarks/totp` on 29 October 2026, in the same run. TOTP runs once per sign-in, so speed was never the reason; the numbers show the replacement costs nothing.

| Operation | pquerna/otp | In-tree | Change |
| --- | --- | --- | --- |
| Generate a code | 534 ns, 11 allocations | 385 ns, 9 allocations | −28% |
| Check a code from the previous step | 1655 ns, 34 allocations | 708 ns, 17 allocations | −57% |

## Phonetic matching

Sound-alike matching finds titles that are spelt differently from what Alexa heard but sound the same. Each word gets one or more phonetic keys, and words with the same key match. English used Double Metaphone from PhonoGrams/double_metaphone, and German used Kölner Phonetik from f1monkey/phonetic. French, Spanish and Italian used English Double Metaphone.

**Why they were replaced.** Both are small libraries, and double_metaphone was on the review's maintenance watch list. They allocated several objects per word, which adds up when a library of 100,000 titles is indexed, and f1monkey/phonetic needed `golang.org/x/exp` for one helper. Owning the encoders also made room for languages no library covered.

**What replaced them.** `internal/fuzzy/phonetic`, with one package per language: `en` and `de` are ports of the two libraries (Apache-2.0 and MIT, with their notices kept) with identical output, and `fr` and `es` are new encoders written from each language's spelling rules. French merges silent final consonants and plural marks, silent h and the -er/-é endings; Spanish merges b and v, silent h, ll and y, soft c, z and s, and a final s or d that many speakers drop. Each language names its own encoder, so adding a language package never changes another's keys.

**How they were checked.** While the libraries were still present, differential tests compared the ports with them on every word of up to four letters, rapid-generated words, invalid UTF-8 and long words. Before the libraries were removed, their output was captured: a digest over every word of up to four letters, a list of about 2000 English and 1600 German generated words, and the keys of every word in the test corpora in all five languages. `benchmarks/phonetic` keeps the side-by-side comparison, adding every word of the matcher's test corpora.

**Speed.** From `benchmarks/phonetic` on 29 October 2026, in the same run, keying title words one at a time:

| Language | Library | In-tree | Change |
| --- | --- | --- | --- |
| English (Double Metaphone) | 444 ns, 208 B, 11 allocations | 80 ns, 5 B, 1 allocation | −82% |
| German (Kölner Phonetik) | 161 ns, 134 B, 3 allocations | 42 ns, 4 B, no allocations | −74% |

When the French and Spanish encoders were added they took about 60 and 50 ns per word, against 79 ns for Double Metaphone.

### Accuracy by language

Each language has a phonetic test corpus: real titles and the ways a speech recogniser plausibly mishears them, tagged by the spelling-to-sound confusion each models, with exact titles and absent titles as controls. The corpora were frozen before any in-tree encoder was evaluated. A case counts as right when Encore makes the expected decision: plays the right title, asks, or says it's not found. `TestPhoneticEval` in `internal/core/match` produces these numbers (`go test -run TestPhoneticEval -v ./internal/core/match/`).

<!-- phonetic-results -->

| Language | Encoder | Cases right | Wrong plays | With English Double Metaphone | With no phonetic keys |
| --- | --- | --- | --- | --- | --- |
| English | Double Metaphone | 172 of 174 | 0 | 172 | 160 |
| German | Kölner Phonetik | 106 of 108 | 0 | 103 | 86 |
| Spanish | Spanish rules | 51 of 55 | 1 | 44 | 41 |
| French | French rules | 61 of 63 | 0 | 55 | 48 |
| Italian | English Double Metaphone | 56 of 57 | 0 | 56 | 49 |

Measured on 29 October 2026 at commit `1473776f`; the inverted index and the simpler trigram retriever give the same counts.

Italian has no encoder of its own yet: English Double Metaphone already makes 56 of its 57 decisions, so an Italian encoder could gain at most one case, too few to show it is worth its rules. It will be revisited when the Italian corpus grows.

French keys merge spellings that are different titles as well as mishearings (*mot* and *mots*, *son* and *sont*), so in French a top candidate that only sounds like the request must lead the runner-up clearly before Encore plays it; otherwise Alexa asks.

<!-- /phonetic-results -->

## What was kept

### miekg/dns

Encore finds Kodi instances on the local network with mDNS, using miekg/dns to build queries and parse answers. Its successor, `codeberg.org/miekg/dns` (v2), was evaluated as the replacement: it added about 300 KiB to the binary and 46 requirements to `go.mod`, and it fails to build a query naming a host whose label contains a dot ("consecutive dots in name"), which Kodi instance names can, ending the whole browse. Encore stays on v1, and tests now pin how such names are queried and how answers parse, byte for byte, so any future swap must match.

### gowebdav

Backups to WebDAV use gowebdav. The alternative considered, emersion's go-webdav, sends uploads of unknown size with chunked transfer encoding. Nextcloud served through PHP-FPM (Apache's mod_proxy_fcgi, or nginx without request buffering) receives an empty body for those, stores a zero-byte file and answers 201 Created, so a backup would look successful and be lost. gowebdav stays, and tests pin that every upload is sent with a Content-Length. The review did fix how Encore used it: each request is now sent once, and uploads stream instead of being held in memory.

## Binary size

A stripped linux/amd64 build without the web interface (`CGO_ENABLED=0 go build -tags noui -trimpath -ldflags '-s -w' ./cmd/encore`, Go 1.27.3) was 38,531,232 bytes before the review (commit `754a911a`) and is 30,150,816 bytes after it (commit `1473776f`), 22% smaller. Encore's `go.mod` lists 58 modules instead of 92. Most of the difference is bleve: dropping it alone took 8.4 MB off.

## Reproducing the numbers

Everything above can be rerun. The [`benchmarks/` README](https://github.com/nightah/encore/tree/main/benchmarks#readme) lists the exact commands for each table, the method (interleaved rounds compared with [benchstat](https://pkg.go.dev/golang.org/x/perf/cmd/benchstat)) and its caveats: times depend on the machine, the Go version and what else is running, so compare runs from one machine, while allocation counts carry over. The parity tests run with `make test-benchmarks`, and in CI on every change.
