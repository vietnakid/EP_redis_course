// cms.go implements a Count-Min Sketch: a depth x width grid of counters
// that answers "roughly how many times have I seen this item?" in fixed
// memory, no matter how many distinct items pass through. A SimpleMap of
// counters is exact but grows with cardinality (one entry per distinct
// item); a sketch of 2000x5 counters is 40KB whether it counts 1000 items
// or 100 million.
//
// Each row has its own hash function, so an item lands in one column per
// row. Increments hit all d cells; a query reads all d cells and takes the
// minimum. Collisions can only ever push a counter *up*, so the minimum is
// the least-polluted estimate available - a sketch therefore never
// underestimates, and overestimates with bounded probability:
//
//	width = 2 / errRate                      (how wrong an estimate can be)
//	depth = log(errProb) / log(1/2)          (how often it's that wrong)
//
// Both are derived on the slides; CalcCMSDim is those two lines.
package datastructure

import (
	"math"

	"github.com/spaolacci/murmur3"
)

// log10PointFive is log10(0.5), precomputed for CalcCMSDim's depth term.
const log10PointFive = -0.30102999566

// CMSStore holds every key whose value is a Count-Min Sketch.
var CMSStore = make(map[string]*CMS)

// CMS is one sketch. counter is [depth][width]: one row per hash
// function, one column per bucket.
type CMS struct {
	width   uint32
	depth   uint32
	counter [][]uint32
	// count is the sum of every increment ever applied - the denominator
	// for turning an estimate into a frequency ("how big a share of the
	// stream was this item?").
	count uint64
}

// CreateCMS allocates a zeroed width x depth sketch. Callers must pass
// non-zero dimensions (the command layer validates this).
func CreateCMS(width uint32, depth uint32) *CMS {
	c := &CMS{
		width:   width,
		depth:   depth,
		counter: make([][]uint32, depth),
	}
	for i := range c.counter {
		c.counter[i] = make([]uint32, width)
	}
	return c
}

// CalcCMSDim converts an error budget into sketch dimensions: errRate is
// how far an estimate may drift (as a fraction of the total count),
// errProb is how often it may drift further than that.
func CalcCMSDim(errRate float64, errProb float64) (uint32, uint32) {
	width := uint32(math.Ceil(2.0 / errRate))
	depth := uint32(math.Ceil(math.Log10(errProb) / log10PointFive))
	return width, depth
}

// hash picks item's column in one row. The row index doubles as the hash
// seed, which is what makes the d rows independent - same item, same
// string, d different columns.
func (c *CMS) hash(item string, row uint32) uint32 {
	hasher := murmur3.New32WithSeed(row)
	hasher.Write([]byte(item))
	return hasher.Sum32()
}

// IncrBy adds value to item's counter in every row and returns the new
// estimate. Counters saturate at MaxUint32 rather than wrapping to 0:
// wrapping would break the "never underestimates" guarantee outright.
func (c *CMS) IncrBy(item string, value uint32) uint32 {
	var minCount uint32 = math.MaxUint32
	for row := range c.depth {
		col := c.hash(item, row) % c.width
		if math.MaxUint32-c.counter[row][col] < value {
			c.counter[row][col] = math.MaxUint32
		} else {
			c.counter[row][col] += value
		}
		if c.counter[row][col] < minCount {
			minCount = c.counter[row][col]
		}
	}
	c.count += uint64(value)
	return minCount
}

// Count returns the estimated number of times item was incremented: the
// minimum across all d rows. Never below the true count, sometimes above.
func (c *CMS) Count(item string) uint32 {
	var minCount uint32 = math.MaxUint32
	for row := range c.depth {
		col := c.hash(item, row) % c.width
		if c.counter[row][col] < minCount {
			minCount = c.counter[row][col]
		}
	}
	return minCount
}

// Info reports the sketch's dimensions and the total of every increment
// applied to it - what CMS.INFO replies with.
func (c *CMS) Info() (width uint32, depth uint32, count uint64) {
	return c.width, c.depth, c.count
}
