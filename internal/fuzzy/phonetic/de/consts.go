package de

const (
	// stackRunes is how many letters fit in the on-stack buffer; longer words spill to the heap.
	stackRunes = 48
	// stackCode is the on-stack capacity of a code; longer codes grow on the heap.
	stackCode = 48
)
