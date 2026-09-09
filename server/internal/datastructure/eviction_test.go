package datastructure

import "testing"

// withEviction resets the string keyspace and eviction state, and installs
// small MaxKeyNumber/EvictionRatio so a handful of keys is enough to
// exercise eviction without inserting a million of them.
func withEviction(t *testing.T, policy EvictionPolicy, maxKeys int, ratio float64) {
	t.Helper()
	origPolicy, origMax, origRatio := ActiveEvictionPolicy, MaxKeyNumber, EvictionRatio
	ActiveEvictionPolicy, MaxKeyNumber, EvictionRatio = policy, maxKeys, ratio
	StringStore = make(map[string]StringEntry)
	lruHead, lruTail = nil, nil
	lruNodes = make(map[string]*lruNode)
	ePool = nil
	EvictedKeys = 0
	t.Cleanup(func() {
		ActiveEvictionPolicy, MaxKeyNumber, EvictionRatio = origPolicy, origMax, origRatio
	})
}

func TestEvictExactLRUEvictsLeastRecentlyUsed(t *testing.T) {
	withEviction(t, EvictionPolicyLRUExact, 4, 0.5)

	for _, k := range []string{"k1", "k2", "k3", "k4"} {
		SetString(k, StringEntry{Value: "v"})
	}
	// Touch k1 then k2, so k3 and k4 become the least recently used.
	GetLiveString("k1")
	GetLiveString("k2")

	// This insert triggers eviction because the store is at MaxKeyNumber.
	SetString("k5", StringEntry{Value: "v"})

	for _, k := range []string{"k3", "k4"} {
		if _, ok := GetLiveString(k); ok {
			t.Errorf("expected %s to be evicted as least recently used, but it is still present", k)
		}
	}
	for _, k := range []string{"k1", "k2", "k5"} {
		if _, ok := GetLiveString(k); !ok {
			t.Errorf("expected %s to survive eviction, but it is missing", k)
		}
	}
}

func TestEvictRandomShrinksStore(t *testing.T) {
	withEviction(t, EvictionPolicyRandom, 4, 0.5)

	for _, k := range []string{"k1", "k2", "k3", "k4"} {
		SetString(k, StringEntry{Value: "v"})
	}
	SetString("k5", StringEntry{Value: "v"})

	if got := len(StringStore); got != 3 {
		t.Errorf("expected 3 keys left after evicting 2 of 5, got %d", got)
	}
}

func TestEvictSampledLRUFavorsRecentlyUsed(t *testing.T) {
	// Sample size 5 with only 4 keys in the store means the pool always
	// contains every key, so sampled LRU should match exact LRU here -
	// the point being to show that the approximation converges to the
	// same answer as the full scan when the sample covers the whole store.
	withEviction(t, EvictionPolicyLRU, 4, 0.5)

	for _, k := range []string{"k1", "k2", "k3", "k4"} {
		SetString(k, StringEntry{Value: "v"})
	}
	GetLiveString("k1")
	GetLiveString("k2")

	SetString("k5", StringEntry{Value: "v"})

	for _, k := range []string{"k3", "k4"} {
		if _, ok := GetLiveString(k); ok {
			t.Errorf("expected %s to be evicted, but it is still present", k)
		}
	}
}

func TestEvictionUpdatesEvictedKeysStat(t *testing.T) {
	withEviction(t, EvictionPolicyLRUExact, 4, 0.5)

	for _, k := range []string{"k1", "k2", "k3", "k4"} {
		SetString(k, StringEntry{Value: "v"})
	}
	SetString("k5", StringEntry{Value: "v"})

	if EvictedKeys != 2 {
		t.Errorf("expected EvictedKeys to be 2, got %d", EvictedKeys)
	}
}

func TestOverwritingExistingKeyNeverEvicts(t *testing.T) {
	withEviction(t, EvictionPolicyLRUExact, 4, 0.5)

	for _, k := range []string{"k1", "k2", "k3", "k4"} {
		SetString(k, StringEntry{Value: "v"})
	}
	SetString("k1", StringEntry{Value: "updated"})

	if len(StringStore) != 4 {
		t.Errorf("expected overwrite to leave store at 4 keys, got %d", len(StringStore))
	}
	if EvictedKeys != 0 {
		t.Errorf("expected no eviction from an overwrite, got EvictedKeys=%d", EvictedKeys)
	}
}
