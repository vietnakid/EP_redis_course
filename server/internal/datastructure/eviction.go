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
// slice kept sorted stalest-first, refreshed with a few freshly sampled
// keys on every eviction pass. See the package doc comment above for why
// this is a good approximation without tracking every key's recency.
//
// idle is a frozen snapshot - computed once, as `sampleTime -
// LastAccessTime`, at the moment a key is (re)sampled into the pool - and
// then left alone. It is deliberately NOT recomputed against a later "now"
// just because real time keeps passing while the key sits in the pool.
//
// This matters because of a specific staleness case: say keyA is accessed
// at t=5 and gets sampled into the pool at t=6 (idle=1). If keyA is
// accessed again at t=10 but never happens to get resampled before the
// next eviction decision, the pool entry still shows idle=1 from the t=6
// snapshot, not "now - 10". keyA can therefore look far more evictable
// than it actually is, right up until populateEPool happens to resample it
// again and overwrite the entry with a fresh idle.
//
// This is not a bug to patch over by recomputing idle from a live
// timestamp on every check - it is the real approximation Redis's sampled
// LRU makes, and exactly why the policy is called *approximate* LRU: a key
// can, in rare cases, be evicted shortly after being touched if it's
// unlucky enough to sit in the pool with a stale snapshot that never gets
// refreshed. Removing that possibility entirely means tracking every key's
// recency all the time, which is exactly what evictExactLRU does instead,
// at higher bookkeeping cost.
type evictionCandidate struct {
	key  string
	idle time.Duration
}

var ePool []evictionCandidate

// sortEPool orders the pool stalest-first: index 0 has the largest idle
// (the oldest snapshot), so it's the next eviction victim.
func sortEPool() {
	sort.Slice(ePool, func(i, j int) bool {
		return ePool[i].idle > ePool[j].idle
	})
}

// populateEPool folds up to EpoolSampleSize freshly sampled keys into the
// pool, keeping it sorted and capped at EpoolMaxSize (dropping the
// freshest entries first - they're the least likely victims).
func populateEPool() {
	now := time.Now()
	remaining := EpoolSampleSize
	for k, e := range StringStore {
		pushToEPool(k, now.Sub(e.LastAccessTime))
		remaining--
		if remaining <= 0 {
			break
		}
	}
}

// pushToEPool records idle for key, overwriting any snapshot already in
// the pool for that same key. Resampling is the only thing that ever
// refreshes an entry - see the idle field's doc comment above.
func pushToEPool(key string, idle time.Duration) {
	for i, c := range ePool {
		if c.key == key {
			ePool[i].idle = idle
			sortEPool()
			return
		}
	}
	ePool = append(ePool, evictionCandidate{key: key, idle: idle})
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
