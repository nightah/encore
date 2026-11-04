package tinyseg

// weights are the original's tables with their keys packed: classes and tags as bytes (tags), characters as runes (chars2, chars3).
type weights struct {
	up [3]map[byte]int32
	bp [2]map[uint32]int32
	uw [6]map[rune]int32
	bw [3]map[uint64]int32
	tw [4]map[uint64]int32
	uc [6]map[byte]int32
	bc [3]map[uint32]int32
	tc [4]map[uint32]int32
	uq [3]map[uint32]int32
	bq [4]map[uint32]int32
	tq [4]map[uint32]int32
}
