// Package command holds the "CPU work" of the server: the in-memory store
// and command dispatch. It has no notion of sockets, goroutines, or event
// loops, so any I/O strategy (thread pool, single-threaded, io-multiplexed)
// can call Handle the same way.
package command

import (
	"math"
	"strconv"
	"strings"
	"time"

	"redis_k2/server/internal/protocol"
)

// entry is a stored value plus its optional expiration. hasExpiry == false
// means the key never expires.
type entry struct {
	value     string
	expireAt  time.Time
	hasExpiry bool
}

// store is a plain package-level map, not wrapped in a mutex: Handle and
// ActiveExpireCycle are only ever called from the single event-loop
// goroutine in main.go (once per readable fd, once per sweep tick,
// strictly sequential) - there is no second goroutine that could ever
// race with this map, so a lock would protect against nothing.
var store = make(map[string]entry)

// getLive returns the entry for key, transparently dropping (and reporting
// absent) anything that has already expired.
func getLive(key string) (entry, bool) {
	e, ok := store[key]
	if !ok {
		return entry{}, false
	}
	if e.hasExpiry && !time.Now().Before(e.expireAt) {
		delete(store, key)
		return entry{}, false
	}
	return e, true
}

func Handle(cmd protocol.Command) []byte {
	if cmd.Name == "" {
		return protocol.NilReply
	}
	switch strings.ToUpper(cmd.Name) {
	case "PING":
		if len(cmd.Args) > 1 {
			return protocol.EncodeError("ERR wrong number of arguments for 'ping' command")
		}
		if len(cmd.Args) == 1 {
			return protocol.EncodeBulkString(cmd.Args[0])
		}
		return protocol.EncodeSimpleString("PONG")
	case "SET":
		return handleSet(cmd.Args)
	case "TTL":
		return handleTTL(cmd.Args, time.Second)
	case "PTTL":
		return handleTTL(cmd.Args, time.Millisecond)
	case "EXPIRE":
		return handleExpire(cmd.Args)
	case "DEL":
		return handleDel(cmd.Args)
	case "EXISTS":
		return handleExists(cmd.Args)
	case "GET":
		if len(cmd.Args) != 1 {
			return protocol.EncodeError("ERR wrong number of arguments for 'GET'")
		}
		e, ok := getLive(cmd.Args[0])
		if !ok {
			return protocol.NilReply
		}
		return protocol.EncodeBulkString(e.value)
	default:
		return protocol.EncodeError("ERR unknown command")
	}
}

// handleSet implements SET key value [EX seconds | PX milliseconds]. args
// is cmd.Args - the command name is already stripped, so args[0] is the
// key and args[1] is the value. No NX/XX/GET/KEEPTTL - just a plain write
// with an optional expiration.
//
// Options are scanned left to right instead of assuming a single fixed
// slot: adding another option later (a bare flag or one that takes its
// own argument) means adding one more case below, not re-deriving a
// magic total argument count. EX and PX are also explicitly mutually
// exclusive - real Redis rejects `SET key value EX 10 PX 20` the same
// way, since only one expiration can apply.
func handleSet(args []string) []byte {
	if len(args) < 2 {
		return protocol.EncodeError("ERR wrong number of arguments for 'SET'")
	}
	key, value := args[0], args[1]
	e := entry{value: value}

	for i := 2; i < len(args); {
		switch strings.ToUpper(args[i]) {
		case "EX", "PX":
			if e.hasExpiry {
				return protocol.EncodeError("ERR syntax error")
			}
			if i+1 >= len(args) {
				return protocol.EncodeError("ERR syntax error")
			}
			dur, err := strconv.ParseInt(args[i+1], 10, 64)
			if err != nil || dur <= 0 {
				return protocol.EncodeError("ERR invalid expire time in 'SET' command")
			}
			unit := time.Second
			if strings.ToUpper(args[i]) == "PX" {
				unit = time.Millisecond
			}
			e.expireAt = time.Now().Add(time.Duration(dur) * unit)
			e.hasExpiry = true
			i += 2
		default:
			return protocol.EncodeError("ERR syntax error")
		}
	}

	store[key] = e
	return protocol.EncodeSimpleString("OK")
}

// handleTTL implements TTL/PTTL: -2 if the key doesn't exist (or already
// expired), -1 if it exists but has no expiration, otherwise the time left
// rounded up to a whole unit (seconds for TTL, milliseconds for PTTL).
// args is cmd.Args - args[0] is the key.
func handleTTL(args []string, unit time.Duration) []byte {
	if len(args) != 1 {
		return protocol.EncodeError("ERR wrong number of arguments for 'TTL'")
	}
	e, ok := getLive(args[0])
	if !ok {
		return protocol.EncodeInteger(-2)
	}
	if !e.hasExpiry {
		return protocol.EncodeInteger(-1)
	}
	remaining := time.Until(e.expireAt)
	if remaining < 0 {
		remaining = 0
	}
	return protocol.EncodeInteger(int64((remaining + unit - 1) / unit))
}

// maxExpireSeconds bounds EXPIRE's seconds argument so seconds*time.Second
// cannot silently overflow int64 - anything past this is already an
// unreachable expiration date, so it is rejected rather than wrapped.
const maxExpireSeconds = math.MaxInt64 / int64(time.Second)

// handleExpire implements EXPIRE key seconds. Returns 1 if the timeout was
// set (or the key was deleted outright, per real Redis, when seconds is
// zero or negative), 0 if the key does not exist. args is cmd.Args -
// args[0] is the key, args[1] is the seconds.
func handleExpire(args []string) []byte {
	if len(args) != 2 {
		return protocol.EncodeError("ERR wrong number of arguments for 'expire' command")
	}
	seconds, err := strconv.ParseInt(args[1], 10, 64)
	if err != nil {
		return protocol.EncodeError("ERR value is not an integer or out of range")
	}
	if seconds > maxExpireSeconds || seconds < -maxExpireSeconds {
		return protocol.EncodeError("ERR invalid expire time in 'expire' command")
	}
	e, ok := getLive(args[0])
	if !ok {
		return protocol.EncodeInteger(0)
	}
	now := time.Now()
	expireAt := now.Add(time.Duration(seconds) * time.Second)
	if !expireAt.After(now) {
		// A non-positive timeout means "expire right now": real Redis
		// deletes the key immediately rather than storing a past
		// expireAt for the next lazy/active sweep to find.
		delete(store, args[0])
		return protocol.EncodeInteger(1)
	}
	e.expireAt = expireAt
	e.hasExpiry = true
	store[args[0]] = e
	return protocol.EncodeInteger(1)
}

// handleDel implements DEL key [key ...]. Returns the number of keys that
// actually existed (and were therefore removed) - keys already absent or
// already expired are not counted, matching real Redis. args is cmd.Args.
func handleDel(args []string) []byte {
	if len(args) < 1 {
		return protocol.EncodeError("ERR wrong number of arguments for 'del' command")
	}
	var removed int64
	for _, key := range args {
		if _, ok := getLive(key); ok {
			delete(store, key)
			removed++
		}
	}
	return protocol.EncodeInteger(removed)
}

// handleExists implements EXISTS key [key ...]. Returns how many of the
// given keys exist; a key repeated in args is counted once per repeat, the
// same as real Redis. args is cmd.Args.
func handleExists(args []string) []byte {
	if len(args) < 1 {
		return protocol.EncodeError("ERR wrong number of arguments for 'exists' command")
	}
	var count int64
	for _, key := range args {
		if _, ok := getLive(key); ok {
			count++
		}
	}
	return protocol.EncodeInteger(count)
}

// activeExpireSampleSize and activeExpireThreshold mirror real Redis's
// active-expire-cycle constants: sample a small, bounded batch of keys
// per pass rather than the whole keyspace, and only take another pass
// immediately if a large fraction of that batch turned out expired
// (meaning there's likely more expired backlog worth clearing now).
const (
	activeExpireSampleSize = 20
	activeExpireThreshold  = 0.25
)

// ActiveExpireCycle proactively evicts keys whose TTL has already elapsed,
// so an idle key doesn't sit in memory forever just because nothing ever
// GETs or TTLs it again. It is a plain function, not a goroutine: the
// event loop in main.go calls it directly between multiplexer.Wait()
// calls, so it runs on the very same single thread as every other command
// - no extra locking model, no background sweeper to reason about.
//
// Unlike a full `range store` sweep, cost per call is bounded regardless
// of total key count: each pass samples at most activeExpireSampleSize
// keys (Go's map iteration order is already randomized per spec, so a
// fresh range each pass is already a fresh random sample - no separate
// shuffle needed). If at least activeExpireThreshold of that sample was
// expired, there's likely more backlog, so it immediately samples again;
// otherwise it stops until the next scheduled tick. This always
// terminates: continuing only ever happens after deleting at least one
// entry, so store strictly shrinks on every pass that doesn't return.
func ActiveExpireCycle() {
	now := time.Now()
	for {
		sampled, expired := 0, 0
		// pick random sample of keys by loop over the map (map iteration order is random)
		// map {a: 1, b: 2, c: 7, d: 9}
		// for (a, b)
		// for (b, c)
		// for (c, d)
		// for (d, a)
		for key, e := range store {
			if sampled >= activeExpireSampleSize {
				break
			}
			sampled++
			if e.hasExpiry && !now.Before(e.expireAt) {
				delete(store, key)
				expired++
			}
		}
		if sampled == 0 || float64(expired)/float64(sampled) < activeExpireThreshold {
			return
		}
	}
}
