// Package retrieval compares Encore's in-tree inverted index with the bleve retriever it replaced, through the same match.Index. The bleve retriever is reconstructed from commit 96d0dfaa and plugged in with match.NewWithRetriever, so normalising, scoring and deciding are shared and only candidate retrieval differs.
package retrieval
