package datastructure

import (
	"os"
	"strings"
)

// SortedSet is a collection of unique string members ordered by an
// associated float64 score. Two implementations exist behind this one
// interface - skiplist.go (what real Redis uses) and bplustree.go (what
// Dragonfly uses) - so command_sortedset.go never has to know which is
// backing a given key.
type SortedSet interface {
	// Add inserts member with score, or updates its score if member is
	// already present. It returns 1 if member is newly added, 0 if it
	// already existed (score changed or not) - matching real ZADD's
	// default return value of "elements added", not "elements changed".
	Add(score float64, member string) int
	Score(member string) (score float64, ok bool)
	// Rank returns member's 0-based position when every member is ordered
	// by score ascending (ties broken by member string).
	Rank(member string) (rank int64, ok bool)
}

// zsetImplEnv selects the sorted-set implementation for every key created
// after the server starts. Real Redis hard-codes skip lists; this server
// exposes the choice so both approaches from the lecture are runnable.
const zsetImplEnv = "ZSET_IMPL"

// defaultBPlusTreeDegree bounds how many keys a B+ tree node holds before
// it splits (M-1 keys, M children). Not its own knob - nobody has asked to
// tune it, and 32 is a reasonable in-memory fanout.
const defaultBPlusTreeDegree = 32

// NewSortedSet builds a fresh, empty sorted set using whichever
// implementation ZSET_IMPL selects ("skiplist", the default, or "btree").
func NewSortedSet() SortedSet {
	if strings.EqualFold(os.Getenv(zsetImplEnv), "btree") {
		return newBPlusZSet(defaultBPlusTreeDegree)
	}
	return newSkiplistZSet()
}

// ZSetStore holds every key whose value is a SortedSet.
var ZSetStore = make(map[string]SortedSet)
