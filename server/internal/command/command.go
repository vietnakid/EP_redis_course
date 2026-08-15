// Package command holds the "CPU work" of the server: the in-memory store
// and command dispatch. It has no notion of sockets, goroutines, or event
// loops, so any I/O strategy (thread pool, single-threaded, io-multiplexed)
// can call Handle the same way.
package command

import (
	"strconv"
	"strings"
	"sync"
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

var store = struct {
	sync.Mutex
	data map[string]entry
}{data: make(map[string]entry)}

// getLive returns the entry for key, transparently dropping (and reporting
// absent) anything that has already expired. Caller must hold store.Lock.
func getLive(key string) (entry, bool) {
	e, ok := store.data[key]
	if !ok {
		return entry{}, false
	}
	if e.hasExpiry && !time.Now().Before(e.expireAt) {
		delete(store.data, key)
		return entry{}, false
	}
	return e, true
}

func Handle(args []string) []byte {
	if len(args) == 0 {
		return protocol.NilReply
	}
	switch strings.ToUpper(args[0]) {
	case "PING":
		if len(args) > 2 {
			return protocol.EncodeError("ERR wrong number of arguments for 'ping' command")
		}
		if len(args) == 2 {
			return protocol.EncodeBulkString(args[1])
		}
		return protocol.EncodeSimpleString("PONG")
	case "SET":
		return handleSet(args)
	case "TTL":
		return handleTTL(args, time.Second)
	case "PTTL":
		return handleTTL(args, time.Millisecond)
	case "GET":
		if len(args) != 2 {
			return protocol.EncodeError("ERR wrong number of arguments for 'GET'")
		}
		store.Lock()
		e, ok := getLive(args[1])
		store.Unlock()
		if !ok {
			return protocol.NilReply
		}
		return protocol.EncodeBulkString(e.value)
	default:
		return protocol.EncodeError("ERR unknown command")
	}
}

// handleSet implements SET key value [EX seconds | PX milliseconds]. No
// NX/XX/GET/KEEPTTL - just a plain write with an optional expiration.
func handleSet(args []string) []byte {
	if len(args) < 3 {
		return protocol.EncodeError("ERR wrong number of arguments for 'SET'")
	}
	key, value := args[1], args[2]

	e := entry{value: value}
	if len(args) > 3 {
		if len(args) != 5 {
			return protocol.EncodeError("ERR syntax error")
		}
		dur, err := strconv.ParseInt(args[4], 10, 64)
		if err != nil || dur <= 0 {
			return protocol.EncodeError("ERR invalid expire time in 'SET' command")
		}
		switch strings.ToUpper(args[3]) {
		case "EX":
			e.expireAt = time.Now().Add(time.Duration(dur) * time.Second)
		case "PX":
			e.expireAt = time.Now().Add(time.Duration(dur) * time.Millisecond)
		default:
			return protocol.EncodeError("ERR syntax error")
		}
		e.hasExpiry = true
	}

	store.Lock()
	store.data[key] = e
	store.Unlock()
	return protocol.EncodeSimpleString("OK")
}

// handleTTL implements TTL/PTTL: -2 if the key doesn't exist (or already
// expired), -1 if it exists but has no expiration, otherwise the time left
// rounded up to a whole unit (seconds for TTL, milliseconds for PTTL).
func handleTTL(args []string, unit time.Duration) []byte {
	if len(args) != 2 {
		return protocol.EncodeError("ERR wrong number of arguments for 'TTL'")
	}
	store.Lock()
	e, ok := getLive(args[1])
	store.Unlock()
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

// ActiveExpireCycle proactively evicts keys whose TTL has already elapsed,
// so an idle key doesn't sit in memory forever just because nothing ever
// GETs or TTLs it again. It is a plain function, not a goroutine: the
// event loop in main.go calls it directly between multiplexer.Wait()
// calls, so it runs on the very same single thread as every other command
// - no extra locking model, no background sweeper to reason about.
func ActiveExpireCycle() {
	now := time.Now()
	store.Lock()
	for key, e := range store.data {
		if e.hasExpiry && !now.Before(e.expireAt) {
			delete(store.data, key)
		}
	}
	store.Unlock()
}
