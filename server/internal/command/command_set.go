// command_set.go handles the set keyspace: SADD, SREM, SISMEMBER,
// SMEMBERS. Each key routes through datastructure.SetStore, created
// lazily on first SADD.
package command

import (
	"redis_k2/server/internal/datastructure"
	"redis_k2/server/internal/protocol"
)

// handleSAdd implements SADD key member [member ...]. args is cmd.Args -
// args[0] is the key, args[1:] are members to add. Returns how many
// members were newly added (already-present members don't count).
func handleSAdd(args []string) []byte {
	if len(args) < 2 {
		return protocol.EncodeError("ERR wrong number of arguments for 'sadd' command")
	}
	key := args[0]
	if reply := checkType(key, datastructure.KindSet); reply != nil {
		return reply
	}
	set, exists := datastructure.SetStore[key]
	if !exists {
		set = datastructure.NewSimpleSet()
		datastructure.SetStore[key] = set
	}
	added := set.Add(args[1:]...)
	return protocol.EncodeInteger(int64(added))
}

// handleSRem implements SREM key member [member ...]. Returns how many
// members were actually removed. A set emptied by SREM is deleted
// outright, matching real Redis: an empty set isn't a value that EXISTS.
func handleSRem(args []string) []byte {
	if len(args) < 2 {
		return protocol.EncodeError("ERR wrong number of arguments for 'srem' command")
	}
	key := args[0]
	if reply := checkType(key, datastructure.KindSet); reply != nil {
		return reply
	}
	set, exists := datastructure.SetStore[key]
	if !exists {
		return protocol.EncodeInteger(0)
	}
	removed := set.Rem(args[1:]...)
	if set.Len() == 0 {
		delete(datastructure.SetStore, key)
	}
	return protocol.EncodeInteger(int64(removed))
}

// handleSIsMember implements SISMEMBER key member: 1 if member is in the
// set, 0 otherwise (including when the key doesn't exist).
func handleSIsMember(args []string) []byte {
	if len(args) != 2 {
		return protocol.EncodeError("ERR wrong number of arguments for 'sismember' command")
	}
	key := args[0]
	if reply := checkType(key, datastructure.KindSet); reply != nil {
		return reply
	}
	set, exists := datastructure.SetStore[key]
	if !exists || !set.IsMember(args[1]) {
		return protocol.EncodeInteger(0)
	}
	return protocol.EncodeInteger(1)
}

// handleSMembers implements SMEMBERS key: every member of the set, in
// unspecified order (an empty array if the key doesn't exist).
func handleSMembers(args []string) []byte {
	if len(args) != 1 {
		return protocol.EncodeError("ERR wrong number of arguments for 'smembers' command")
	}
	key := args[0]
	if reply := checkType(key, datastructure.KindSet); reply != nil {
		return reply
	}
	set, exists := datastructure.SetStore[key]
	if !exists {
		return protocol.Encode([]protocol.Value{})
	}
	members := set.Members()
	values := make([]protocol.Value, len(members))
	for i, m := range members {
		values[i] = m
	}
	return protocol.Encode(values)
}
