// eviction.go implements memory-pressure eviction for the string keyspace
// (StringStore). Sets/sorted sets/Bloom/CMS are demonstration structures
// for their own lectures, not the "hot" keyspace under memory pressure, so
// - matching real Redis's allkeys-* vs volatile-* split in spirit, but kept
// to a single store for a teachable, single-threaded example - eviction
// only ever looks at StringStore.
//
// Three interchangeable policies pick the victim once StringStore hits
// MaxKeyNumber:
//   - EvictionPolicyRandom  : whatever a map range happens to hand back
//     first. Cheapest possible policy, worst hit rate - the baseline the
//     other two are judged against.
//   - EvictionPolicyLRU     : Redis's real-world compromise. Don't track
//     every key's recency; just resample a handful of keys into a small
//     pool on each eviction and evict the oldest of that sample. Converges
//     toward exact LRU as the sample grows, without paying for a global
//     structure.
//   - EvictionPolicyLRUExact: the textbook version. A doubly linked list
//     kept in perfect recency order (see lru_list.go) so the true
//     least-recently-used key is always the tail - O(1) to find, O(1) to
//     unlink. This is the ground truth EvictionPolicyLRU approximates.
package datastructure

import (
	"sort"
	"time"
)

// MaxKeyNumber and EvictionRatio are vars, not consts, so tests (and a
// future config file) can dial them down instead of inserting a million
// keys to exercise eviction.
var (
	MaxKeyNumber  = 1000000
	EvictionRatio = 0.1
)

// EpoolMaxSize bounds the sampled-LRU pool; EpoolSampleSize is how many
// fresh keys get folded into it on each eviction pass. Both mirror real
// Redis's maxmemory-samples knob.
const (
	EpoolMaxSize    = 16
	EpoolSampleSize = 5
)

type EvictionPolicy int

const (
	EvictionPolicyRandom EvictionPolicy = iota
	EvictionPolicyLRU
	EvictionPolicyLRUExact
)

func (p EvictionPolicy) String() string {
	switch p {
	case EvictionPolicyRandom:
		return "allkeys-random"
	case EvictionPolicyLRU:
		return "allkeys-lru"
	case EvictionPolicyLRUExact:
		return "allkeys-lru-exact"
	default:
		return "unknown"
	}
}

// ActiveEvictionPolicy selects which victim-picking strategy SetString
// triggers once StringStore is full.
var ActiveEvictionPolicy = EvictionPolicyLRU

// EvictedKeys counts every key evicted so far, exposed via INFO's Stats
// section so eviction is observable at runtime instead of only in logs.
var EvictedKeys int64

// maybeEvict runs before inserting incomingKey. Overwriting an existing key
// never grows the store, so only a genuinely new key can trigger eviction.
func maybeEvict(incomingKey string) {
	if _, exists := StringStore[incomingKey]; exists {
		return
	}
	if len(StringStore) < MaxKeyNumber {
		return
	}
	switch ActiveEvictionPolicy {
	case EvictionPolicyRandom:
		evictRandom()
	case EvictionPolicyLRU:
		evictSampledLRU()
	case EvictionPolicyLRUExact:
		evictExactLRU()
	}
}

func victimCount() int {
	return int(EvictionRatio * float64(MaxKeyNumber))
}

func deleteEvicted(key string) {
	delete(StringStore, key)
	lruRemove(key)
	EvictedKeys++
}

func evictRandom() {
	remaining := victimCount()
	for k := range StringStore {
		deleteEvicted(k)
		remaining--
		if remaining <= 0 {
			break
		}
	}
}

// evictionCandidate and ePool implement the sampled-LRU pool: a small
// slice kept sorted oldest-first, refreshed with a few random keys on every
// eviction pass. See the package doc comment above for why this is a good
// approximation without tracking every key's recency.
type evictionCandidate struct {
	key            string
	lastAccessTime time.Time
}

var ePool []evictionCandidate

func sortEPool() {
	sort.Slice(ePool, func(i, j int) bool {
		return ePool[i].lastAccessTime.Before(ePool[j].lastAccessTime)
	})
}

// populateEPool folds up to EpoolSampleSize freshly sampled keys into the
// pool, keeping it sorted and capped at EpoolMaxSize (dropping the
// most-recently-used entries first - they're the least likely victims).
func populateEPool() {
	remaining := EpoolSampleSize
	for k, e := range StringStore {
		pushToEPool(k, e.LastAccessTime)
		remaining--
		if remaining <= 0 {
			break
		}
	}
}

func pushToEPool(key string, lastAccessTime time.Time) {
	for i, c := range ePool {
		if c.key == key {
			ePool[i].lastAccessTime = lastAccessTime
			sortEPool()
			return
		}
	}
	ePool = append(ePool, evictionCandidate{key: key, lastAccessTime: lastAccessTime})
	sortEPool()
	if len(ePool) > EpoolMaxSize {
		ePool = ePool[:EpoolMaxSize]
	}
}

func evictSampledLRU() {
	populateEPool()
	remaining := victimCount()
	for remaining > 0 && len(ePool) > 0 {
		victim := ePool[0]
		ePool = ePool[1:]
		deleteEvicted(victim.key)
		remaining--
	}
}

func evictExactLRU() {
	remaining := victimCount()
	for remaining > 0 {
		victim, ok := lruVictim()
		if !ok {
			break
		}
		deleteEvicted(victim)
		remaining--
	}
}
