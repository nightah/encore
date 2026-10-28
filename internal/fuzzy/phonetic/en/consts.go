package en

const (
	// stackRunes is how many letters fit in the on-stack buffer; longer words spill to the heap.
	stackRunes = 48
	// stackCode is the on-stack capacity of each code; codes grow past it on the heap.
	stackCode = 48
)
