// Package command holds the "CPU work" of the server: command dispatch
// over the stores in internal/datastructure. It has no notion of sockets,
// goroutines, or event loops, so any I/O strategy (thread pool,
// single-threaded, io-multiplexed) can call Handle the same way.
//
// Handle routes a parsed protocol.Command to one of three handler files
// split by the keyspace it touches: command_map.go (plain strings -
// SET/GET/TTL/EXPIRE/DEL/EXISTS), command_set.go (SADD/SREM/SISMEMBER/
// SMEMBERS), command_sortedset.go (ZADD/ZSCORE/ZRANK). Each handler talks
// to its keyspace only through internal/datastructure - this file and its
// siblings never touch a store map directly except through it.
package command

import (
	"strings"
	"time"

	"redis_k2/server/internal/datastructure"
	"redis_k2/server/internal/protocol"
)

// wrongTypeErr matches real Redis's error for e.g. SADD against a key
// that already holds a string.
const wrongTypeErr = "WRONGTYPE Operation against a key holding the wrong kind of value"

// checkType returns a WRONGTYPE reply if key already exists as a
// different kind, or nil if the caller may proceed (key is absent or
// already the right kind).
func checkType(key string, want datastructure.Kind) []byte {
	if kind := datastructure.KindOf(key); kind != datastructure.KindNone && kind != want {
		return protocol.EncodeError(wrongTypeErr)
	}
	return nil
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
	case "GET":
		return handleGet(cmd.Args)
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
	case "SADD":
		return handleSAdd(cmd.Args)
	case "SREM":
		return handleSRem(cmd.Args)
	case "SISMEMBER":
		return handleSIsMember(cmd.Args)
	case "SMEMBERS":
		return handleSMembers(cmd.Args)
	case "ZADD":
		return handleZAdd(cmd.Args)
	case "ZSCORE":
		return handleZScore(cmd.Args)
	case "ZRANK":
		return handleZRank(cmd.Args)
	default:
		return protocol.EncodeError("ERR unknown command")
	}
}
