package datastructure

import (
	"math"
	"testing"
)

func TestCMSIncrByCount(t *testing.T) {
	c := CreateCMS(200, 5)

	if got := c.Count("a"); got != 0 {
		t.Fatalf("Count(a) on empty sketch = %d, want 0", got)
	}
	if got := c.IncrBy("a", 3); got != 3 {
		t.Fatalf("IncrBy(a, 3) = %d, want 3", got)
	}
	if got := c.IncrBy("a", 2); got != 5 {
		t.Fatalf("IncrBy(a, 2) = %d, want 5", got)
	}
	// A sketch overestimates at worst, so an item incremented 5 times can
	// never read back below 5.
	if got := c.Count("a"); got < 5 {
		t.Fatalf("Count(a) = %d, want >= 5 (a sketch must never underestimate)", got)
	}

	width, depth, count := c.Info()
	if width != 200 || depth != 5 || count != 5 {
		t.Fatalf("Info() = (%d, %d, %d), want (200, 5, 5)", width, depth, count)
	}
}

// Counters saturate instead of wrapping: wrapping to 0 would break the
// never-underestimate guarantee.
func TestCMSSaturates(t *testing.T) {
	c := CreateCMS(16, 2)
	c.IncrBy("a", math.MaxUint32)
	c.IncrBy("a", 10)
	if got := c.Count("a"); got != math.MaxUint32 {
		t.Fatalf("Count(a) after overflow = %d, want MaxUint32", got)
	}
}

func TestCalcCMSDim(t *testing.T) {
	// width = ceil(2/0.001) = 2000, depth = ceil(log10(0.01)/log10(0.5)) = 7
	width, depth := CalcCMSDim(0.001, 0.01)
	if width != 2000 || depth != 7 {
		t.Fatalf("CalcCMSDim(0.001, 0.01) = (%d, %d), want (2000, 7)", width, depth)
	}
}
