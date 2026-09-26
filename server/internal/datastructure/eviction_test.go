package datastructure

import (
	"testing"
	"time"
)

// withEviction returns a fresh Store with small MaxKeyNumber/EvictionRatio
// so a handful of keys is enough to exercise eviction without inserting a
// million of them.
func withEviction(policy EvictionPolicy, maxKeys int, ratio float64) *Store {
	s := NewStore()
	s.ActiveEvictionPolicy = policy
	s.MaxKeyNumber = maxKeys
	s.EvictionRatio = ratio
	return s
}

func TestEvictExactLRUEvictsLeastRecentlyUsed(t *testing.T) {
	s := withEviction(EvictionPolicyLRUExact, 4, 0.5)

	for _, k := range []string{"k1", "k2", "k3", "k4"} {
		s.SetString(k, StringEntry{Value: "v"})
	}
	// Touch k1 then k2, so k3 and k4 become the least recently used.
	s.GetLiveString("k1")
	s.GetLiveString("k2")

	// This insert triggers eviction because the store is at MaxKeyNumber.
	s.SetString("k5", StringEntry{Value: "v"})

	for _, k := range []string{"k3", "k4"} {
		if _, ok := s.GetLiveString(k); ok {
			t.Errorf("expected %s to be evicted as least recently used, but it is still present", k)
		}
	}
	for _, k := range []string{"k1", "k2", "k5"} {
		if _, ok := s.GetLiveString(k); !ok {
			t.Errorf("expected %s to survive eviction, but it is missing", k)
		}
	}
}

func TestEvictRandomShrinksStore(t *testing.T) {
	s := withEviction(EvictionPolicyRandom, 4, 0.5)

	for _, k := range []string{"k1", "k2", "k3", "k4"} {
		s.SetString(k, StringEntry{Value: "v"})
	}
	s.SetString("k5", StringEntry{Value: "v"})

	if got := len(s.StringStore); got != 3 {
		t.Errorf("expected 3 keys left after evicting 2 of 5, got %d", got)
	}
}

func TestEvictSampledLRUFavorsRecentlyUsed(t *testing.T) {
	// Sample size 5 with only 4 keys in the store means the pool always
	// contains every key, so sampled LRU should match exact LRU here -
	// the point being to show that the approximation converges to the
	// same answer as the full scan when the sample covers the whole store.
	s := withEviction(EvictionPolicyLRU, 4, 0.5)

	for _, k := range []string{"k1", "k2", "k3", "k4"} {
		s.SetString(k, StringEntry{Value: "v"})
	}
	s.GetLiveString("k1")
	s.GetLiveString("k2")

	s.SetString("k5", StringEntry{Value: "v"})

	for _, k := range []string{"k3", "k4"} {
		if _, ok := s.GetLiveString(k); ok {
			t.Errorf("expected %s to be evicted, but it is still present", k)
		}
	}
}

func TestEvictionUpdatesEvictedKeysStat(t *testing.T) {
	s := withEviction(EvictionPolicyLRUExact, 4, 0.5)

	for _, k := range []string{"k1", "k2", "k3", "k4"} {
		s.SetString(k, StringEntry{Value: "v"})
	}
	s.SetString("k5", StringEntry{Value: "v"})

	if s.EvictedKeys != 2 {
		t.Errorf("expected EvictedKeys to be 2, got %d", s.EvictedKeys)
	}
}

// TestSampledLRUPoolIdleIsFrozenSnapshot pins down the staleness behavior
// documented on evictionCandidate: a pool entry's idle is a snapshot taken
// when the key was (re)sampled, and must NOT silently grow just because
// real time passes while it sits in the pool untouched. Only an explicit
// resample (another pushToEPool call for the same key) may update it -
// mirroring real Redis, and the reason this policy is "approximate": a key
// re-accessed after being sampled can still look stale in the pool until
// it happens to be resampled again.
func TestSampledLRUPoolIdleIsFrozenSnapshot(t *testing.T) {
	s := NewStore()

	s.pushToEPool("keyA", 1*time.Second)
	frozen := s.ePool[0].idle

	time.Sleep(5 * time.Millisecond)
	if s.ePool[0].idle != frozen {
		t.Fatalf("idle changed with no resample: got %v, want frozen %v", s.ePool[0].idle, frozen)
	}

	s.pushToEPool("keyA", 10*time.Millisecond)
	if s.ePool[0].idle != 10*time.Millisecond {
		t.Fatalf("resample did not refresh idle: got %v, want 10ms", s.ePool[0].idle)
	}
}

func TestOverwritingExistingKeyNeverEvicts(t *testing.T) {
	s := withEviction(EvictionPolicyLRUExact, 4, 0.5)

	for _, k := range []string{"k1", "k2", "k3", "k4"} {
		s.SetString(k, StringEntry{Value: "v"})
	}
	s.SetString("k1", StringEntry{Value: "updated"})

	if len(s.StringStore) != 4 {
		t.Errorf("expected overwrite to leave store at 4 keys, got %d", len(s.StringStore))
	}
	if s.EvictedKeys != 0 {
		t.Errorf("expected no eviction from an overwrite, got EvictedKeys=%d", s.EvictedKeys)
	}
}
