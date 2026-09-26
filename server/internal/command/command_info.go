// command_info.go implements INFO: a plain-text status report so eviction
// and keyspace behavior are observable at runtime instead of only visible
// in server logs, matching the shape (not the full breadth) of real
// Redis's INFO sections.
//
// In the shared-nothing engine, INFO is dispatched to one worker like any
// other keyless command (see server.dispatch), so its Keyspace/Stats
// numbers reflect only that worker's partition, not the whole server -
// there is no cross-worker aggregation, by the same shared-nothing design
// that gives every worker its own lock-free Store.
package command

import (
	"bytes"
	"fmt"

	"redis_k2/server/internal/datastructure"
	"redis_k2/server/internal/protocol"
)

func handleInfo(store *datastructure.Store, args []string) []byte {
	var buf bytes.Buffer

	// Keyspace counts every key across every kind; eviction below only
	// ever acts on the string keyspace (see eviction.go's doc comment).
	totalKeys := len(store.StringStore) +
		len(store.SetStore) +
		len(store.ZSetStore) +
		len(store.BloomStore) +
		len(store.CMSStore)

	buf.WriteString("# Keyspace\r\n")
	fmt.Fprintf(&buf, "db0:keys=%d,expires=0,avg_ttl=0\r\n", totalKeys)

	buf.WriteString("# Memory\r\n")
	fmt.Fprintf(&buf, "maxmemory_policy:%s\r\n", store.ActiveEvictionPolicy)
	fmt.Fprintf(&buf, "maxkeys:%d\r\n", store.MaxKeyNumber)

	buf.WriteString("# Stats\r\n")
	fmt.Fprintf(&buf, "evicted_keys:%d\r\n", store.EvictedKeys)

	return protocol.EncodeBulkString(buf.String())
}
