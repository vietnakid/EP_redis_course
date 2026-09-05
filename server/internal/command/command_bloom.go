// command_bloom.go handles the Bloom-filter keyspace: BF.RESERVE,
// BF.MADD, BF.EXISTS. Each key routes through datastructure.BloomStore.
// This is the minimal teaching subset of RedisBloom - BF.ADD/BF.INSERT/
// BF.INFO and scaling filters are deliberately out of scope.
package command

import (
	"strconv"

	"redis_k2/server/internal/datastructure"
	"redis_k2/server/internal/protocol"
)

// Defaults RedisBloom uses when BF.MADD has to create the filter itself.
// A filter's size is fixed at creation, so these are a guess at the
// caller's capacity - a real workload should BF.RESERVE with its own
// numbers before the first add.
const (
	defaultErrorRate = 0.01
	defaultCapacity  = 100
)

// handleBFReserve implements BF.RESERVE key error_rate capacity: size and
// allocate an empty filter. Unlike SADD-style lazy creation this refuses
// to touch an existing key, because silently re-creating the filter would
// throw away every item already in it.
func handleBFReserve(args []string) []byte {
	if len(args) != 3 {
		return protocol.EncodeError("ERR wrong number of arguments for 'bf.reserve' command")
	}
	key := args[0]
	if reply := checkType(key, datastructure.KindBloom); reply != nil {
		return reply
	}
	if _, exists := datastructure.BloomStore[key]; exists {
		return protocol.EncodeError("ERR item exists")
	}
	errorRate, err := strconv.ParseFloat(args[1], 64)
	if err != nil {
		return protocol.EncodeError("ERR bad error rate")
	}
	// errorRate == 0 would demand an infinitely large filter, and >= 1
	// would demand a negative one: both are rejected here, before
	// CreateBloomFilter turns them into an absurd allocation.
	if errorRate <= 0 || errorRate >= 1 {
		return protocol.EncodeError("ERR (0 < error rate range < 1)")
	}
	capacity, err := strconv.ParseUint(args[2], 10, 64)
	if err != nil {
		return protocol.EncodeError("ERR value is not an integer or out of range")
	}
	if capacity == 0 {
		return protocol.EncodeError("ERR (capacity should be larger than 0)")
	}
	datastructure.BloomStore[key] = datastructure.CreateBloomFilter(capacity, errorRate)
	return protocol.EncodeSimpleString("OK")
}

// handleBFMAdd implements BF.MADD key item [item ...], creating the filter
// with the default sizing if the key doesn't exist yet. Replies with one
// integer per item: 1 if the item was definitely not in the filter before,
// 0 if it probably already was.
func handleBFMAdd(args []string) []byte {
	if len(args) < 2 {
		return protocol.EncodeError("ERR wrong number of arguments for 'bf.madd' command")
	}
	key := args[0]
	if reply := checkType(key, datastructure.KindBloom); reply != nil {
		return reply
	}
	bloom, exists := datastructure.BloomStore[key]
	if !exists {
		bloom = datastructure.CreateBloomFilter(defaultCapacity, defaultErrorRate)
		datastructure.BloomStore[key] = bloom
	}
	replies := make([]protocol.Value, 0, len(args)-1)
	for _, item := range args[1:] {
		var added int64
		if bloom.AddIfNotExist(item) {
			added = 1
		}
		replies = append(replies, added)
	}
	return protocol.Encode(replies)
}

// handleBFExists implements BF.EXISTS key item: 1 if item may be in the
// filter, 0 if it definitely isn't. A missing key is an empty filter, so
// it answers 0 rather than erroring.
func handleBFExists(args []string) []byte {
	if len(args) != 2 {
		return protocol.EncodeError("ERR wrong number of arguments for 'bf.exists' command")
	}
	key := args[0]
	if reply := checkType(key, datastructure.KindBloom); reply != nil {
		return reply
	}
	bloom, exists := datastructure.BloomStore[key]
	if !exists || !bloom.Exist(args[1]) {
		return protocol.EncodeInteger(0)
	}
	return protocol.EncodeInteger(1)
}
