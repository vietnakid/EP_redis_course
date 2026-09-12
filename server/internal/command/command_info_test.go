// INFO's tests live in their own file rather than command_test.go since
// they need the datastructure package's eviction knobs, which the other
// command tests never touch.
package command

import (
	"strings"
	"testing"

	"redis_k2/server/internal/datastructure"
)

func TestInfoReportsKeyspaceAndEviction(t *testing.T) {
	origMax, origRatio, origPolicy := datastructure.MaxKeyNumber, datastructure.EvictionRatio, datastructure.ActiveEvictionPolicy
	t.Cleanup(func() {
		datastructure.MaxKeyNumber, datastructure.EvictionRatio, datastructure.ActiveEvictionPolicy = origMax, origRatio, origPolicy
	})
	datastructure.MaxKeyNumber = 50
	datastructure.ActiveEvictionPolicy = datastructure.EvictionPolicyRandom

	handle(t, "SET TestInfo:a 1")
	handle(t, "SET TestInfo:b 2")

	got := handle(t, "INFO")
	if !strings.Contains(got, "# Keyspace") {
		t.Errorf("INFO reply missing Keyspace section: %q", got)
	}
	if !strings.Contains(got, "maxmemory_policy:allkeys-random") {
		t.Errorf("INFO reply missing eviction policy: %q", got)
	}
	if !strings.Contains(got, "maxkeys:50") {
		t.Errorf("INFO reply missing maxkeys: %q", got)
	}
	if !strings.Contains(got, "evicted_keys:") {
		t.Errorf("INFO reply missing evicted_keys: %q", got)
	}
}
