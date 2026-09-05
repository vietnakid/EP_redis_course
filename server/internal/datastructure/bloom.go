// bloom.go implements a Bloom filter: a bit array plus k hash functions
// that answers "have I seen this item?" with two possible answers -
// "definitely not" or "probably yes". It never returns a false negative,
// it sometimes returns a false positive, and in exchange it stores no
// items at all. A SimpleSet of 1M 20-byte members costs ~20MB+; a Bloom
// filter sized for 1M items at a 1% false-positive rate costs ~1.2MB and
// never grows with member length.
//
// Sizing follows http://en.wikipedia.org/wiki/Bloom_filter:
//
//	bits   = -(entries * ln(errorRate)) / ln(2)^2
//	hashes = (bits / entries) * ln(2)
//
// Both are derived on the slides; calcBpe and CreateBloomFilter below are
// those two lines of algebra, nothing more.
package datastructure

import (
	"math"

	"github.com/spaolacci/murmur3"
)

const (
	ln2       float64 = 0.693147180559945
	ln2Square float64 = 0.480453013918201

	// bloomSeed is an arbitrary fixed seed. Fixed, not random, so the same
	// item always lands on the same bits across runs - a filter that
	// reseeded per process would forget everything it had "learned".
	bloomSeed uint32 = 0x9747b28c
)

// BloomStore holds every key whose value is a Bloom filter. Like the other
// stores in this package it is an unguarded map: only the event-loop
// goroutine in main.go ever touches it.
var BloomStore = make(map[string]*Bloom)

// Bloom is one filter. bf is the bit array, packed 8 bits per byte -
// []bool would work and be shorter, but it would spend a whole byte per
// bit and throw away the 8x memory win that is the entire point of the
// structure.
type Bloom struct {
	Hashes      int     // k: how many bit positions per item
	Entries     uint64  // n: capacity the filter was sized for
	ErrorRate   float64 // p: target false-positive rate at n entries
	bitPerEntry float64 // bits/n, kept for reference
	bf          []uint8
	bits        uint64 // len(bf) in bits
	bytes       uint64 // len(bf) in bytes
}

// calcBpe returns the optimal bits-per-entry for a target error rate:
// -ln(p) / ln(2)^2.
func calcBpe(errorRate float64) float64 {
	return math.Abs(math.Log(errorRate) / ln2Square)
}

// CreateBloomFilter sizes and allocates a filter for entries items at the
// given errorRate. The bit count is rounded up to a whole 64-bit word so
// the bit array is word-aligned; callers must pass entries > 0 and
// 0 < errorRate < 1 (the command layer validates this - errorRate == 0
// would make calcBpe return +Inf and allocate until the process dies).
func CreateBloomFilter(entries uint64, errorRate float64) *Bloom {
	b := &Bloom{
		Entries:     entries,
		ErrorRate:   errorRate,
		bitPerEntry: calcBpe(errorRate),
	}
	bits := uint64(float64(entries) * b.bitPerEntry)
	if bits%64 != 0 {
		b.bytes = ((bits / 64) + 1) * 8
	} else {
		b.bytes = bits / 8
	}
	b.bits = b.bytes * 8
	b.Hashes = int(math.Ceil(ln2 * b.bitPerEntry))
	b.bf = make([]uint8, b.bytes)
	return b
}

// hash returns the two 64-bit halves of one murmur3-128 digest. k
// independent hashes are then synthesised as a + b*i (the
// Kirsch-Mitzenmacher trick) instead of running k separate hash
// functions: one hash computation per item regardless of k, with the same
// false-positive behaviour asymptotically.
func (b *Bloom) hash(item string) (uint64, uint64) {
	hasher := murmur3.New128WithSeed(bloomSeed)
	hasher.Write([]byte(item))
	return hasher.Sum128()
}

// bitPos returns the i-th bit index for a digest, plus the byte holding it
// and the mask selecting it inside that byte.
func (b *Bloom) bitPos(h1, h2 uint64, i int) (byteIdx uint64, mask uint8) {
	pos := (h1 + h2*uint64(i)) % b.bits
	return pos >> 3, 1 << (pos % 8) // pos>>3 == pos/8, pos%8 == bit in byte
}

// AddIfNotExist sets all k bits for item and reports whether the item was
// new - true if at least one of those bits was still 0, i.e. the filter had
// definitely not seen item before. false means "probably already added",
// carrying the same false-positive risk as Exist: the bits may have been
// set by other items.
func (b *Bloom) AddIfNotExist(item string) bool {
	h1, h2 := b.hash(item)
	added := false
	for i := range b.Hashes {
		byteIdx, mask := b.bitPos(h1, h2, i)
		if b.bf[byteIdx]&mask == 0 {
			added = true
		}
		b.bf[byteIdx] |= mask
	}
	return added
}

// Exist reports whether item may be in the filter. false is certain (some
// bit is 0, so item was never added); true is probabilistic.
func (b *Bloom) Exist(item string) bool {
	h1, h2 := b.hash(item)
	for i := range b.Hashes {
		byteIdx, mask := b.bitPos(h1, h2, i)
		if b.bf[byteIdx]&mask == 0 {
			return false
		}
	}
	return true
}
