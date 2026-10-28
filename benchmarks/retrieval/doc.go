// Package retrieval compares Encore's in-tree inverted index with a bleve retriever, through the same match.Index. The bleve retriever has the mapping, analyzers and query Encore used with bleve and is plugged in with match.NewWithRetriever, so normalising, scoring and deciding are shared and only candidate retrieval differs.
package retrieval
