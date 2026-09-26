// Package command holds the "CPU work" of the server: command dispatch
// over a *datastructure.Store. It has no notion of sockets, goroutines, or
// event loops, so any I/O strategy (thread pool, single-threaded,
// io-multiplexed, shared-nothing workers) can call Handle the same way,
// each against whichever Store it owns.
//
// Handle routes a parsed protocol.Command to one of six handler files
// split by the keyspace it touches: command_map.go (plain strings -
// SET/GET/TTL/EXPIRE/DEL/EXISTS, and the only keyspace eviction acts on -
// see datastructure/eviction.go), command_set.go (SADD/SREM/SISMEMBER/
// SMEMBERS), command_sortedset.go (ZADD/ZSCORE/ZRANK), command_bloom.go
// (BF.RESERVE/BF.MADD/BF.EXISTS), command_cms.go (CMS.INITBYDIM/
// CMS.INITBYPROB/CMS.INCRBY/CMS.QUERY/CMS.INFO) and command_info.go
// (INFO, spanning every keyspace). Each handler talks to its keyspace only
// through the *datastructure.Store passed into it - this file and its
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
func checkType(store *datastructure.Store, key string, want datastructure.Kind) []byte {
	if kind := store.KindOf(key); kind != datastructure.KindNone && kind != want {
		return protocol.EncodeError(wrongTypeErr)
	}
	return nil
}

func Handle(store *datastructure.Store, cmd protocol.Command) []byte {
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
	case "INFO":
		return handleInfo(store, cmd.Args)
	case "SET":
		return handleSet(store, cmd.Args)
	case "GET":
		return handleGet(store, cmd.Args)
	case "TTL":
		return handleTTL(store, cmd.Args, time.Second)
	case "PTTL":
		return handleTTL(store, cmd.Args, time.Millisecond)
	case "EXPIRE":
		return handleExpire(store, cmd.Args)
	case "DEL":
		return handleDel(store, cmd.Args)
	case "EXISTS":
		return handleExists(store, cmd.Args)
	case "SADD":
		return handleSAdd(store, cmd.Args)
	case "SREM":
		return handleSRem(store, cmd.Args)
	case "SISMEMBER":
		return handleSIsMember(store, cmd.Args)
	case "SMEMBERS":
		return handleSMembers(store, cmd.Args)
	case "ZADD":
		return handleZAdd(store, cmd.Args)
	case "ZSCORE":
		return handleZScore(store, cmd.Args)
	case "ZRANK":
		return handleZRank(store, cmd.Args)
	case "BF.RESERVE":
		return handleBFReserve(store, cmd.Args)
	case "BF.MADD":
		return handleBFMAdd(store, cmd.Args)
	case "BF.EXISTS":
		return handleBFExists(store, cmd.Args)
	case "CMS.INITBYDIM":
		return handleCMSInitByDim(store, cmd.Args)
	case "CMS.INITBYPROB":
		return handleCMSInitByProb(store, cmd.Args)
	case "CMS.INCRBY":
		return handleCMSIncrBy(store, cmd.Args)
	case "CMS.QUERY":
		return handleCMSQuery(store, cmd.Args)
	case "CMS.INFO":
		return handleCMSInfo(store, cmd.Args)
	default:
		return protocol.EncodeError("ERR unknown command")
	}
}
