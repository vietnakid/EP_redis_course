// command_info.go implements INFO: a plain-text status report so eviction
// and keyspace behavior are observable at runtime instead of only visible
// in server logs, matching the shape (not the full breadth) of real
// Redis's INFO sections.
package command

import (
	"bytes"
	"fmt"

	"redis_k2/server/internal/datastructure"
	"redis_k2/server/internal/protocol"
)

func handleInfo(args []string) []byte {
	var buf bytes.Buffer

	// Keyspace counts every key across every kind; eviction below only
	// ever acts on the string keyspace (see eviction.go's doc comment).
	totalKeys := len(datastructure.StringStore) +
		len(datastructure.SetStore) +
		len(datastructure.ZSetStore) +
		len(datastructure.BloomStore) +
		len(datastructure.CMSStore)

	buf.WriteString("# Keyspace\r\n")
	fmt.Fprintf(&buf, "db0:keys=%d,expires=0,avg_ttl=0\r\n", totalKeys)

	buf.WriteString("# Memory\r\n")
	fmt.Fprintf(&buf, "maxmemory_policy:%s\r\n", datastructure.ActiveEvictionPolicy)
	fmt.Fprintf(&buf, "maxkeys:%d\r\n", datastructure.MaxKeyNumber)

	buf.WriteString("# Stats\r\n")
	fmt.Fprintf(&buf, "evicted_keys:%d\r\n", datastructure.EvictedKeys)

	return protocol.EncodeBulkString(buf.String())
}
