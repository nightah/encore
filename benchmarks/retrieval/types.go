package retrieval

import "github.com/blevesearch/bleve/v2"

// Bleve is a match.Retriever backed by an in-memory bleve index, with the mapping, analyzers, batching and query Encore used with bleve.
type Bleve struct {
	idx bleve.Index
}

// phoneticFilter replaces each token with its phonetic keys at the same position.
type phoneticFilter struct {
	fn func(string) []string
}
