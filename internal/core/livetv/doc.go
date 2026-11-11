// Package livetv finds a media centre's live TV channels and what's on them from what a listener said.
//
// A Guide belongs to one media centre instance. It keeps the channel list for a while and indexes the names with the library matcher, so spoken numbers ("nine go", "ten drama") and near misses find channels as they find titles. What's on now changes by the minute, so the guide is asked for it again within a minute, and never kept past the end of the programme on now that ends first.
//
// Channels are found by name first: "seven" is the channel named Seven, wherever it is numbered. A number finds the channel with that number only when no channel is named it. When two channels share a name or number, the lowest numbered wins, then one the guide has something on now for, then the lowest ID, so the same request always tunes the same channel.
package livetv
