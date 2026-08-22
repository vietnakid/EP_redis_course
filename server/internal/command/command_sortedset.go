// command_sortedset.go handles the sorted-set keyspace: ZADD, ZSCORE,
// ZRANK. Each key routes through datastructure.ZSetStore, created lazily
// on first ZADD using whichever implementation ZSET_IMPL selects.
package command

import (
	"strconv"

	"redis_k2/server/internal/datastructure"
	"redis_k2/server/internal/protocol"
)

// handleZAdd implements ZADD key score member [score member ...]. args is
// cmd.Args - args[0] is the key, followed by one or more (score, member)
// pairs. Returns how many members were newly added (a member that already
// existed just gets its score updated, and doesn't count).
func handleZAdd(args []string) []byte {
	if len(args) < 3 || len(args)%2 != 1 {
		return protocol.EncodeError("ERR wrong number of arguments for 'zadd' command")
	}
	key := args[0]
	if reply := checkType(key, datastructure.KindZSet); reply != nil {
		return reply
	}

	// Parse every (score, member) pair before mutating anything, so a bad
	// score partway through the command doesn't leave a half-applied ZADD.
	type pair struct {
		score  float64
		member string
	}
	pairs := make([]pair, 0, (len(args)-1)/2)
	for i := 1; i < len(args); i += 2 {
		score, err := strconv.ParseFloat(args[i], 64)
		if err != nil {
			return protocol.EncodeError("ERR value is not a valid float")
		}
		pairs = append(pairs, pair{score: score, member: args[i+1]})
	}

	zset, exists := datastructure.ZSetStore[key]
	if !exists {
		zset = datastructure.NewSortedSet()
		datastructure.ZSetStore[key] = zset
	}
	var added int64
	for _, p := range pairs {
		added += int64(zset.Add(p.score, p.member))
	}
	return protocol.EncodeInteger(added)
}

// handleZScore implements ZSCORE key member: member's score as a bulk
// string, or a nil reply if the key or member doesn't exist.
func handleZScore(args []string) []byte {
	if len(args) != 2 {
		return protocol.EncodeError("ERR wrong number of arguments for 'zscore' command")
	}
	key, member := args[0], args[1]
	if reply := checkType(key, datastructure.KindZSet); reply != nil {
		return reply
	}
	zset, exists := datastructure.ZSetStore[key]
	if !exists {
		return protocol.NilReply
	}
	score, ok := zset.Score(member)
	if !ok {
		return protocol.NilReply
	}
	return protocol.EncodeBulkString(strconv.FormatFloat(score, 'f', -1, 64))
}

// handleZRank implements ZRANK key member: member's 0-based rank with
// scores ordered low to high, or a nil reply if the key or member doesn't
// exist.
func handleZRank(args []string) []byte {
	if len(args) != 2 {
		return protocol.EncodeError("ERR wrong number of arguments for 'zrank' command")
	}
	key, member := args[0], args[1]
	if reply := checkType(key, datastructure.KindZSet); reply != nil {
		return reply
	}
	zset, exists := datastructure.ZSetStore[key]
	if !exists {
		return protocol.NilReply
	}
	rank, ok := zset.Rank(member)
	if !ok {
		return protocol.NilReply
	}
	return protocol.EncodeInteger(rank)
}
