package datastructure

import "time"

// StringEntry is a stored string value plus its optional expiration.
// HasExpiry == false means the key never expires.
type StringEntry struct {
	Value     string
	ExpireAt  time.Time
	HasExpiry bool
}

// StringStore holds every key whose value is a plain string. It is a
// plain package-level map, not wrapped in a mutex: every store in this
// package is only ever touched from the single event-loop goroutine in
// main.go, so a lock would protect against nothing.
var StringStore = make(map[string]StringEntry)

// GetLiveString returns the entry for key, transparently dropping (and
// reporting absent) anything that has already expired.
func GetLiveString(key string) (StringEntry, bool) {
	e, ok := StringStore[key]
	if !ok {
		return StringEntry{}, false
	}
	if e.HasExpiry && !time.Now().Before(e.ExpireAt) {
		delete(StringStore, key)
		return StringEntry{}, false
	}
	return e, true
}

// activeExpireSampleSize and activeExpireThreshold mirror real Redis's
// active-expire-cycle constants: sample a small, bounded batch of keys per
// pass rather than the whole keyspace, and only take another pass
// immediately if a large fraction of that batch turned out expired
// (meaning there's likely more expired backlog worth clearing now).
const (
	activeExpireSampleSize = 20
	activeExpireThreshold  = 0.25
)

// ActiveExpireCycle proactively evicts keys whose TTL has already elapsed,
// so an idle key doesn't sit in memory forever just because nothing ever
// GETs or TTLs it again. main.go's event loop calls this directly between
// multiplexer.Wait() calls - same goroutine as every command, no extra
// locking model, no background sweeper to reason about.
//
// Cost per call is bounded regardless of total key count: each pass
// samples at most activeExpireSampleSize keys (Go's map iteration order is
// already randomized per spec, so a fresh range each pass is already a
// fresh random sample - no separate shuffle needed). If at least
// activeExpireThreshold of that sample was expired, there's likely more
// backlog, so it immediately samples again; otherwise it stops until the
// next scheduled tick.
func ActiveExpireCycle() {
	now := time.Now()
	for {
		sampled, expired := 0, 0
		for key, e := range StringStore {
			if sampled >= activeExpireSampleSize {
				break
			}
			sampled++
			if e.HasExpiry && !now.Before(e.ExpireAt) {
				delete(StringStore, key)
				expired++
			}
		}
		if sampled == 0 || float64(expired)/float64(sampled) < activeExpireThreshold {
			return
		}
	}
}
