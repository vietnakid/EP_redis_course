// command_map.go handles the plain-string keyspace: SET, GET, TTL/PTTL,
// EXPIRE, DEL, EXISTS. DEL and EXISTS span all three keyspaces (matching
// real Redis: DEL doesn't care what type a key holds); SET/GET only ever
// touch datastructure.StringStore.
package command

import (
	"math"
	"strconv"
	"strings"
	"time"

	"redis_k2/server/internal/datastructure"
	"redis_k2/server/internal/protocol"
)

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
	e := datastructure.StringEntry{Value: value}

	for i := 2; i < len(args); {
		switch strings.ToUpper(args[i]) {
		case "EX", "PX":
			if e.HasExpiry {
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
			e.ExpireAt = time.Now().Add(time.Duration(dur) * unit)
			e.HasExpiry = true
			i += 2
		default:
			return protocol.EncodeError("ERR syntax error")
		}
	}

	// SET always overwrites whatever type the key held before - clear out
	// any stale set/sorted-set left behind by an earlier SADD/ZADD.
	datastructure.ReplaceKind(key, datastructure.KindString)
	datastructure.SetString(key, e)
	return protocol.EncodeSimpleString("OK")
}

func handleGet(args []string) []byte {
	if len(args) != 1 {
		return protocol.EncodeError("ERR wrong number of arguments for 'GET'")
	}
	if reply := checkType(args[0], datastructure.KindString); reply != nil {
		return reply
	}
	e, ok := datastructure.GetLiveString(args[0])
	if !ok {
		return protocol.NilReply
	}
	return protocol.EncodeBulkString(e.Value)
}

// handleTTL implements TTL/PTTL: -2 if the key doesn't exist (or already
// expired), -1 if it exists but has no expiration, otherwise the time left
// rounded up to a whole unit (seconds for TTL, milliseconds for PTTL).
// args is cmd.Args - args[0] is the key.
func handleTTL(args []string, unit time.Duration) []byte {
	if len(args) != 1 {
		return protocol.EncodeError("ERR wrong number of arguments for 'TTL'")
	}
	e, ok := datastructure.GetLiveString(args[0])
	if !ok {
		return protocol.EncodeInteger(-2)
	}
	if !e.HasExpiry {
		return protocol.EncodeInteger(-1)
	}
	remaining := time.Until(e.ExpireAt)
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
	e, ok := datastructure.GetLiveString(args[0])
	if !ok {
		return protocol.EncodeInteger(0)
	}
	now := time.Now()
	expireAt := now.Add(time.Duration(seconds) * time.Second)
	if !expireAt.After(now) {
		// A non-positive timeout means "expire right now": real Redis
		// deletes the key immediately rather than storing a past
		// expireAt for the next lazy/active sweep to find.
		datastructure.Delete(args[0])
		return protocol.EncodeInteger(1)
	}
	e.ExpireAt = expireAt
	e.HasExpiry = true
	datastructure.StringStore[args[0]] = e
	return protocol.EncodeInteger(1)
}

// handleDel implements DEL key [key ...] across every keyspace: a key
// holding a set or sorted set is just as deletable as one holding a
// string. Returns the number of keys that actually existed.
func handleDel(args []string) []byte {
	if len(args) < 1 {
		return protocol.EncodeError("ERR wrong number of arguments for 'del' command")
	}
	var removed int64
	for _, key := range args {
		if datastructure.Delete(key) {
			removed++
		}
	}
	return protocol.EncodeInteger(removed)
}

// handleExists implements EXISTS key [key ...] across every keyspace.
// Returns how many of the given keys exist; a key repeated in args is
// counted once per repeat, the same as real Redis.
func handleExists(args []string) []byte {
	if len(args) < 1 {
		return protocol.EncodeError("ERR wrong number of arguments for 'exists' command")
	}
	var count int64
	for _, key := range args {
		if datastructure.Exists(key) {
			count++
		}
	}
	return protocol.EncodeInteger(count)
}
