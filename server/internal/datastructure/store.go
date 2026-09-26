package datastructure

// Store holds one independent copy of every keyspace a key can live in,
// plus the state eviction and exact-LRU tracking need to act on it. It
// exists so shared-nothing worker isolation is just "give each Worker its
// own *Store" - nothing in this package reaches for a package-level map
// anymore, so there is nothing left for two Workers to accidentally share.
type Store struct {
	StringStore map[string]StringEntry
	SetStore    map[string]*SimpleSet
	ZSetStore   map[string]SortedSet
	BloomStore  map[string]*Bloom
	CMSStore    map[string]*CMS

	// MaxKeyNumber/EvictionRatio/ActiveEvictionPolicy/EvictedKeys used to be
	// package vars a test could dial down directly; now they're just
	// fields, so a test (or a Worker with a smaller partition) sets them on
	// its own Store instead of mutating shared package state.
	MaxKeyNumber         int
	EvictionRatio        float64
	ActiveEvictionPolicy EvictionPolicy
	EvictedKeys          int64
	ePool                []evictionCandidate

	lruHead, lruTail *lruNode
	lruNodes         map[string]*lruNode
}

// NewStore returns an empty Store with the same eviction defaults every
// lecture up to now hard-coded as package vars.
func NewStore() *Store {
	return &Store{
		StringStore:          make(map[string]StringEntry),
		SetStore:             make(map[string]*SimpleSet),
		ZSetStore:            make(map[string]SortedSet),
		BloomStore:           make(map[string]*Bloom),
		CMSStore:             make(map[string]*CMS),
		MaxKeyNumber:         1000000,
		EvictionRatio:        0.1,
		ActiveEvictionPolicy: EvictionPolicyLRU,
		lruNodes:             make(map[string]*lruNode),
	}
}
