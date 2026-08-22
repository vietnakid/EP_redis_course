package datastructure

import "testing"

// impls covers both SortedSet backends with the same test bodies below -
// the whole point of the SortedSet interface is that callers (and this
// test) can't tell them apart.
var impls = map[string]func() SortedSet{
	"skiplist": func() SortedSet { return newSkiplistZSet() },
	"btree":    func() SortedSet { return newBPlusZSet(4) }, // small degree: forces splits fast
}

func TestSortedSetAddScoreRank(t *testing.T) {
	for name, newSet := range impls {
		t.Run(name, func(t *testing.T) {
			z := newSet()

			if added := z.Add(1, "a"); added != 1 {
				t.Fatalf("Add new member: got %d, want 1", added)
			}
			if added := z.Add(1, "a"); added != 0 {
				t.Fatalf("Add existing member: got %d, want 0", added)
			}

			z.Add(3, "c")
			z.Add(2, "b")

			for member, want := range map[string]float64{"a": 1, "b": 2, "c": 3} {
				got, ok := z.Score(member)
				if !ok || got != want {
					t.Fatalf("Score(%q) = %v, %v; want %v, true", member, got, ok, want)
				}
			}
			if _, ok := z.Score("missing"); ok {
				t.Fatal("Score(missing) reported ok")
			}

			for member, want := range map[string]int64{"a": 0, "b": 1, "c": 2} {
				got, ok := z.Rank(member)
				if !ok || got != want {
					t.Fatalf("Rank(%q) = %v, %v; want %v, true", member, got, ok, want)
				}
			}
			if _, ok := z.Rank("missing"); ok {
				t.Fatal("Rank(missing) reported ok")
			}
		})
	}
}

// TestSortedSetUpdateMovesRank re-adds an existing member with a score
// that relocates it across the ordering - the case the lecture slides
// call out ("check if member already exists - yes: update score"), and
// the case the B+ tree implementation this was ported from got wrong (it
// searched the leaf for the *new* score instead of relocating the
// existing item, silently duplicating members whose score changed enough
// to land in a different leaf).
func TestSortedSetUpdateMovesRank(t *testing.T) {
	for name, newSet := range impls {
		t.Run(name, func(t *testing.T) {
			z := newSet()
			for i, m := range []string{"a", "b", "c", "d", "e"} {
				z.Add(float64(i), m)
			}

			// "a" starts lowest (rank 0); push it above everything else.
			if added := z.Add(100, "a"); added != 0 {
				t.Fatalf("Add on existing member returned %d, want 0", added)
			}

			score, ok := z.Score("a")
			if !ok || score != 100 {
				t.Fatalf("Score(a) after update = %v, %v; want 100, true", score, ok)
			}
			rank, ok := z.Rank("a")
			if !ok || rank != 4 {
				t.Fatalf("Rank(a) after update = %v, %v; want 4, true", rank, ok)
			}
			// "b" should now be lowest.
			if rank, ok := z.Rank("b"); !ok || rank != 0 {
				t.Fatalf("Rank(b) after a's update = %v, %v; want 0, true", rank, ok)
			}
		})
	}
}

// TestSortedSetManyMembersStaySorted forces several node splits (btree
// with degree 4 splits above 3 items per node) and checks rank still
// reflects true score order afterward.
func TestSortedSetManyMembersStaySorted(t *testing.T) {
	for name, newSet := range impls {
		t.Run(name, func(t *testing.T) {
			z := newSet()
			const n = 50
			members := make([]string, n)
			for i := range n {
				m := string(rune('A' + i%26))
				if i >= 26 {
					m += string(rune('0' + i/26))
				}
				members[i] = m
				z.Add(float64(n-i), m) // insert in descending score order
			}

			for i, m := range members {
				wantRank := int64(n - 1 - i) // lowest score (added last) is rank 0
				rank, ok := z.Rank(m)
				if !ok || rank != wantRank {
					t.Fatalf("Rank(%q) = %v, %v; want %v, true", m, rank, ok, wantRank)
				}
			}
		})
	}
}

func TestNewSortedSetSelectsImplFromEnv(t *testing.T) {
	t.Setenv(zsetImplEnv, "btree")
	if _, ok := NewSortedSet().(*bPlusZSet); !ok {
		t.Fatal("ZSET_IMPL=btree did not select bPlusZSet")
	}

	t.Setenv(zsetImplEnv, "skiplist")
	if _, ok := NewSortedSet().(*skiplistZSet); !ok {
		t.Fatal("ZSET_IMPL=skiplist did not select skiplistZSet")
	}

	t.Setenv(zsetImplEnv, "")
	if _, ok := NewSortedSet().(*skiplistZSet); !ok {
		t.Fatal("unset ZSET_IMPL did not default to skiplistZSet")
	}
}
