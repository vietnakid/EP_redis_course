package command

import (
	"strings"
	"testing"

	"redis_k2/server/internal/protocol"
)

// handle parses and runs one RESP command line, e.g. "SADD myset a b".
func handle(t *testing.T, line string) string {
	t.Helper()
	fields := strings.Fields(line)
	if len(fields) == 0 {
		t.Fatalf("empty command line")
	}
	cmd := protocol.Command{Name: fields[0], Args: fields[1:]}
	return string(Handle(cmd))
}

func TestSetCommands(t *testing.T) {
	key := "TestSetCommands:s"

	if got := handle(t, "SADD "+key+" a b a"); got != ":2\r\n" {
		t.Fatalf("SADD = %q, want :2 (a counted once)", got)
	}
	if got := handle(t, "SISMEMBER "+key+" a"); got != ":1\r\n" {
		t.Fatalf("SISMEMBER a = %q, want :1", got)
	}
	if got := handle(t, "SISMEMBER "+key+" z"); got != ":0\r\n" {
		t.Fatalf("SISMEMBER z = %q, want :0", got)
	}
	if got := handle(t, "SREM "+key+" a"); got != ":1\r\n" {
		t.Fatalf("SREM a = %q, want :1", got)
	}
	if got := handle(t, "SMEMBERS "+key); got != "*1\r\n$1\r\nb\r\n" {
		t.Fatalf("SMEMBERS = %q, want *1\\r\\n$1\\r\\nb\\r\\n", got)
	}
}

func TestSortedSetCommands(t *testing.T) {
	key := "TestSortedSetCommands:z"

	if got := handle(t, "ZADD "+key+" 1 a 2 b"); got != ":2\r\n" {
		t.Fatalf("ZADD = %q, want :2", got)
	}
	if got := handle(t, "ZSCORE "+key+" a"); got != "$1\r\n1\r\n" {
		t.Fatalf("ZSCORE a = %q, want $1\\r\\n1\\r\\n", got)
	}
	if got := handle(t, "ZRANK "+key+" b"); got != ":1\r\n" {
		t.Fatalf("ZRANK b = %q, want :1", got)
	}
	if got := handle(t, "ZSCORE "+key+" missing"); got != string(protocol.NilReply) {
		t.Fatalf("ZSCORE missing = %q, want nil reply", got)
	}
}

func TestWrongType(t *testing.T) {
	key := "TestWrongType:key"
	handle(t, "SET "+key+" hello")

	for _, line := range []string{"SADD " + key + " m", "ZADD " + key + " 1 m", "SISMEMBER " + key + " m"} {
		got := handle(t, line)
		if !strings.HasPrefix(got, "-WRONGTYPE") {
			t.Errorf("%s = %q, want WRONGTYPE error", line, got)
		}
	}

	// A plain SET always overwrites the type. Real Redis then treats the
	// key as a plain string (SISMEMBER on it is WRONGTYPE, not 0) - the
	// real test is that the stale SetStore entry doesn't survive to
	// resurface once the string is deleted.
	setKey := "TestWrongType:set"
	handle(t, "SADD "+setKey+" m")
	handle(t, "SET "+setKey+" now-a-string")
	if got := handle(t, "GET "+setKey); got != "$12\r\nnow-a-string\r\n" {
		t.Fatalf("GET after SET-over-set = %q", got)
	}
	if got := handle(t, "SISMEMBER "+setKey+" m"); !strings.HasPrefix(got, "-WRONGTYPE") {
		t.Fatalf("SISMEMBER on now-string key = %q, want WRONGTYPE", got)
	}
	handle(t, "DEL "+setKey)
	if got := handle(t, "EXISTS "+setKey); got != ":0\r\n" {
		t.Fatalf("EXISTS after DEL = %q, want :0 (a stale SetStore entry resurfaced)", got)
	}
}

func TestDelExistsAcrossKinds(t *testing.T) {
	strKey, setKey, zsetKey := "TestDelExists:s", "TestDelExists:set", "TestDelExists:z"
	handle(t, "SET "+strKey+" v")
	handle(t, "SADD "+setKey+" m")
	handle(t, "ZADD "+zsetKey+" 1 m")

	if got := handle(t, "EXISTS "+strKey+" "+setKey+" "+zsetKey+" missing"); got != ":3\r\n" {
		t.Fatalf("EXISTS = %q, want :3", got)
	}
	if got := handle(t, "DEL "+strKey+" "+setKey+" "+zsetKey+" missing"); got != ":3\r\n" {
		t.Fatalf("DEL = %q, want :3", got)
	}
	if got := handle(t, "EXISTS "+strKey+" "+setKey+" "+zsetKey); got != ":0\r\n" {
		t.Fatalf("EXISTS after DEL = %q, want :0", got)
	}
}
