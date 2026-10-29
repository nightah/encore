# Benchmarks

Encore implements a few things in-tree that it used to take from libraries: candidate retrieval for title matching (bleve), the scorer's edit distances (go-edlib), TOTP (pquerna/otp) and the phonetic encoders (PhonoGrams/double_metaphone and f1monkey/phonetic). This directory holds the code that compares each one with the library it replaced, so anyone can check the claims in the documentation's [Dependencies and benchmarks](https://nightah.github.io/encore/contributing/dependencies/) page and rerun the numbers.

It is a Go module of its own (`github.com/nightah/encore/benchmarks`), so the replaced libraries and benchstat (`golang.org/x/perf`) are only ever in `benchmarks/go.mod`, never in Encore's `go.mod` or binary. A `replace` directive points it at the repository root, and Go's `internal` rule is path based, so it imports Encore's internal packages directly and measures the code that ships, not a copy.

## What each package compares

| Package | In-tree code | Compared with | Parity tests | Benchmarks |
| --- | --- | --- | --- | --- |
| `retrieval` | The inverted index in `internal/core/match` | bleve 2.6.1, set up as Encore v0.1.2 used it | Every ASR corpus case and every library title as a query (English and German suites): the same candidates and the same `Search` result. A 10,000-title scale library with 874 queries: the same candidates with a pool as large as the library, and the same decision, top candidate, score and margin at the default pool of 800 | `Search100k`, `SearchScale`, `Candidates`, `Build` |
| `editdistance` | `jaroWinkler` and `osaDistance` in `internal/core/match` | go-edlib 1.7.0 (`JaroWinklerSimilarity`, `OSADamerauLevenshteinDistance`) | Bit-for-bit equal results for every pair of strings of up to three runes over `abü😀`, for rapid-generated text (arbitrary Unicode, invalid UTF-8, strings longer than the 64-rune stack buffers) and for title pairs | `JaroWinkler`, `OSADistance` |
| `totp` | RFC 6238 TOTP in `internal/auth` | pquerna/otp 1.5.0 with the options Encore used (SHA1, six digits, 30-second steps, one step of skew) | RFC 6238 Appendix B vectors for both; the same code for random secrets and times; the same accept or reject for codes from nearby steps, random codes and codes with surrounding space; the same secret and `otpauth://` URL from the same random bytes | `Code`, `Validate` |
| `phonetic` | `internal/fuzzy/phonetic/en` (Double Metaphone) and `de` (Kölner Phonetik) | PhonoGrams/double_metaphone 0.1.0 and f1monkey/phonetic 1.4.2, keyed as `internal/core/match` called them (the matcher's own rule for a German final -er sits outside the encoder) | The same keys for every word of up to four letters (a to z, plus ä, ö, ü and ß for German), for rapid-generated words, invalid UTF-8 and long words, and for every word of the matcher's ASR, phonetic and grown corpora | `Keys` per language |

Each benchmark runs the library and the in-tree code as sub-benchmarks named `impl=<library>` and `impl=intree`, library first, so `benchstat -col /impl` reports the in-tree code's change against the library.

### Phonetic accuracy

Parity shows the ports behave like the libraries; it doesn't show which encoder suits which language. That is measured in the root module, not here, because the evaluation swaps the encoder inside the matcher, which only `internal/core/match`'s own tests can reach, and the French and Spanish encoders, written from each language's spelling rules, have no library to compare with. From the repository root:

```sh
go test -tags phoneticeval -run 'TestPhonetic(Eval|Distractors)' -v ./internal/core/match/
```

`TestPhoneticEval` scores every language's original and grown corpora (`internal/core/match/testdata/asr_corpus.yaml`, `testdata/phonetic/` and `testdata/grown/`) with the production encoders, English Double Metaphone everywhere and no phonetic keys, and lists every case each one gains or loses against English. `TestPhoneticDistractors` counts wrong plays for 2,000 absent titles per language, built from a vocabulary frozen in `internal/core/match/testdata/distractors/`. Both hold production to the counts the documentation publishes: `TestPhoneticEval` runs its gates in the normal test run, and the build tag adds the trigram retriever and the encoders it only reports, which a CI job of its own runs with `TestPhoneticDistractors`, so they fail the pull request that loses accuracy rather than a nightly run; the nightly benchmarks job only checks parity with the pinned libraries. The documentation's [accuracy tables](https://nightah.github.io/encore/contributing/dependencies/#accuracy-by-language) come from these two tests, with how the intervals and p-values were computed.

### Seams in Encore's code

Retrieval, the edit distances and TOTP are unexported, so two files export them without changing behaviour. Encore itself never calls them:

- `internal/core/match/external.go`: `NewWithRetriever` builds an `Index` whose candidate retrieval is supplied from outside, so bleve runs through the same normalising, scoring and deciding as the inverted index; `Index.Candidates` returns what retrieval hands the scorer; `JaroWinkler` and `OSADistance` wrap the scorer's functions.
- `internal/auth/external.go`: `NewTOTPKey`, `TOTPCode` and `TOTPStep` wrap what enrolment and sign-in use.

The phonetic encoders are exported already.

## Running

Parity tests, which CI runs in short mode and the nightly workflow in full:

```sh
make test-benchmarks        # or: cd benchmarks && go test ./...
```

A quick look at every comparison (one run each, not enough for conclusions):

```sh
make bench
```

### Reproducing the published numbers

The documentation's tables come from eight interleaved rounds, one process per package per round, so a burst of load on the machine hits both sides of a comparison rather than one. From `benchmarks/`:

```sh
mkdir -p bin out
for p in retrieval editdistance totp phonetic; do go test -c -o bin/$p.test ./$p; done
for round in 1 2 3 4 5 6 7 8; do
  (cd retrieval && ENCORE_BENCH_SCALE=100000,500000 ../bin/retrieval.test -test.run '^$' \
    -test.bench 'Search100k|SearchScale|Candidates' -test.benchtime 1000x) >> out/retrieval.txt
  (cd retrieval && ../bin/retrieval.test -test.run '^$' -test.bench Build -test.benchtime 3x) >> out/build.txt
  for p in editdistance totp phonetic; do (cd $p && ../bin/$p.test -test.run '^$' -test.bench .) >> out/$p.txt; done
done
for f in out/*.txt; do go tool benchstat -col /impl "$f"; done
```

The test binaries run inside their package directory because `retrieval` and `phonetic` read the matcher's corpora from `internal/core/match/testdata`. The raw output of the published run is in [`results/`](results/), one directory per date, with the load average before each round in `load.txt`; `go tool benchstat -col /impl results/<date>/retrieval.txt` prints its table again.

`ENCORE_BENCH_SCALE` lists the library sizes for `SearchScale` and `Candidates` (default 100000). Each process builds every library once per retriever and shares it across `-count` runs and sub-benchmarks; at 500,000 titles the two retrievers together hold a few GiB.

### What the benchmarks measure

- `Search100k`: whole searches (retrieval, scoring, deciding) over 100,000 made-up titles, querying exact titles, one-letter typos, first words and "seinfield". The library and queries are those of `BenchmarkSearch100k` in `internal/core/match`.
- `SearchScale`: whole searches over the scale library: the English ASR corpus inside synthetic titles whose common words follow a Zipf distribution, as real libraries' do, queried with 700 synthetic mishearings (typos, sound-alikes, merged and dropped words) and the 174 corpus cases. It is `BenchmarkSearchScale`'s library, generated by a copy of `scaleDocs` and `scaleQueries` from `internal/core/match/scale_test.go`.
- `Candidates`: retrieval alone over the scale library: normalising the query and finding the 800 candidates the scorer would get.
- `Build`: indexing the 100,000 titles of `Search100k` from empty. `heap-MB` is the live heap afterwards; bleve may still be merging segments in the background when it is read.

`p50-ms` and `p99-ms` are the median and 99th percentile latency of the individual operations within one run, next to Go's mean `sec/op`.

### Comparisons that need Encore's history

Some results compare Encore with an earlier version of itself rather than with a library, so they are rerun from git history with the in-package harness in `internal/core/match`:

- Compaction pauses of the inverted index: `go test -run '^$' -bench BenchmarkInvertedCompact -benchtime 3x -count 8 ./internal/core/match/` with `ENCORE_MATCH_BENCH_SCALE=100000,500000`. The "before" figures on the docs page come from compacting under the write lock, which the code no longer does.
- The candidate pool size: `ENCORE_MATCH_SCALE=10000,100000,500000 ENCORE_MATCH_POOLS=200,400,800,1600 go test -run TestRetrieverScale -v -timeout 2h ./internal/core/match/`. Exhaustive scoring at 500,000 titles takes minutes; set `ENCORE_MATCH_SCALE_CACHE` to a directory to keep its results between runs.
- Binary size: `CGO_ENABLED=0 go build -trimpath -ldflags '-s -w' -o encore ./cmd/encore` at two commits.

## Caveats

- Numbers depend on the machine, the Go version and what else is running. The published run records its CPU (in the benchmark output), Go version and load; compare runs from the same machine only, and prefer benchstat's significance over single runs.
- Allocation counts (`B/op`, `allocs/op`) are stable across machines; times are not.
- The bleve retriever is set up as Encore v0.1.2 used it, on top of today's matcher, so normalising and scoring are today's for both sides. bleve adds the IDF of a filter of several kinds to candidates' retrieval scores and the inverted index doesn't; the parity tests use no filter or one kind, where that makes no difference.
- The replaced libraries are pinned to the versions Encore last shipped (Renovate leaves them alone), so the comparisons keep measuring what was replaced.
