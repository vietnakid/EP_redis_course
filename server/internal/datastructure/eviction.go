// eviction.go implements memory-pressure eviction for the string keyspace
// (StringStore). Sets/sorted sets/Bloom/CMS are demonstration structures
// for their own lectures, not the "hot" keyspace under memory pressure, so
// - matching real Redis's allkeys-* vs volatile-* split in spirit, but kept
// to a single store for a teachable example - eviction only ever looks at
// StringStore.
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

// maybeEvict runs before inserting incomingKey. Overwriting an existing key
// never grows the store, so only a genuinely new key can trigger eviction.
func (s *Store) maybeEvict(incomingKey string) {
	if _, exists := s.StringStore[incomingKey]; exists {
		return
	}
	if len(s.StringStore) < s.MaxKeyNumber {
		return
	}
	switch s.ActiveEvictionPolicy {
	case EvictionPolicyRandom:
		s.evictRandom()
	case EvictionPolicyLRU:
		s.evictSampledLRU()
	case EvictionPolicyLRUExact:
		s.evictExactLRU()
	}
}

func (s *Store) victimCount() int {
	return int(s.EvictionRatio * float64(s.MaxKeyNumber))
}

func (s *Store) deleteEvicted(key string) {
	delete(s.StringStore, key)
	s.lruRemove(key)
	s.EvictedKeys++
}

func (s *Store) evictRandom() {
	remaining := s.victimCount()
	for k := range s.StringStore {
		s.deleteEvicted(k)
		remaining--
		if remaining <= 0 {
			break
		}
	}
}

// evictionCandidate and Store.ePool implement the sampled-LRU pool: a small
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

// sortEPool orders the pool stalest-first: index 0 has the largest idle
// (the oldest snapshot), so it's the next eviction victim.
func (s *Store) sortEPool() {
	sort.Slice(s.ePool, func(i, j int) bool {
		return s.ePool[i].idle > s.ePool[j].idle
	})
}

// populateEPool folds up to EpoolSampleSize freshly sampled keys into the
// pool, keeping it sorted and capped at EpoolMaxSize (dropping the
// freshest entries first - they're the least likely victims).
func (s *Store) populateEPool() {
	now := time.Now()
	remaining := EpoolSampleSize
	for k, e := range s.StringStore {
		s.pushToEPool(k, now.Sub(e.LastAccessTime))
		remaining--
		if remaining <= 0 {
			break
		}
	}
}

// pushToEPool records idle for key, overwriting any snapshot already in
// the pool for that same key. Resampling is the only thing that ever
// refreshes an entry - see the idle field's doc comment above.
func (s *Store) pushToEPool(key string, idle time.Duration) {
	for i, c := range s.ePool {
		if c.key == key {
			s.ePool[i].idle = idle
			s.sortEPool()
			return
		}
	}
	s.ePool = append(s.ePool, evictionCandidate{key: key, idle: idle})
	s.sortEPool()
	if len(s.ePool) > EpoolMaxSize {
		s.ePool = s.ePool[:EpoolMaxSize]
	}
}

func (s *Store) evictSampledLRU() {
	s.populateEPool()
	remaining := s.victimCount()
	for remaining > 0 && len(s.ePool) > 0 {
		victim := s.ePool[0]
		s.ePool = s.ePool[1:]
		s.deleteEvicted(victim.key)
		remaining--
	}
}

func (s *Store) evictExactLRU() {
	remaining := s.victimCount()
	for remaining > 0 {
		victim, ok := s.lruVictim()
		if !ok {
			break
		}
		s.deleteEvicted(victim)
		remaining--
	}
}
