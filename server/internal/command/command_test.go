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

func TestBloomCommands(t *testing.T) {
	key := "TestBloomCommands:bf"

	if got := handle(t, "BF.RESERVE "+key+" 0.01 1000"); got != "+OK\r\n" {
		t.Fatalf("BF.RESERVE = %q, want +OK", got)
	}
	if got := handle(t, "BF.RESERVE "+key+" 0.01 1000"); got != "-ERR item exists\r\n" {
		t.Fatalf("BF.RESERVE twice = %q, want item exists error", got)
	}
	if got := handle(t, "BF.MADD "+key+" a b a"); got != "*3\r\n:1\r\n:1\r\n:0\r\n" {
		t.Fatalf("BF.MADD = %q, want [1 1 0] (a already added the second time)", got)
	}
	if got := handle(t, "BF.EXISTS "+key+" a"); got != ":1\r\n" {
		t.Fatalf("BF.EXISTS a = %q, want :1", got)
	}
	if got := handle(t, "BF.EXISTS "+key+" nope"); got != ":0\r\n" {
		t.Fatalf("BF.EXISTS nope = %q, want :0", got)
	}

	// Bad sizing is rejected before it reaches CreateBloomFilter.
	if got := handle(t, "BF.RESERVE TestBloomCommands:bad 0 100"); got != "-ERR (0 < error rate range < 1)\r\n" {
		t.Fatalf("BF.RESERVE error_rate=0 = %q, want error-rate error", got)
	}
	if got := handle(t, "BF.RESERVE TestBloomCommands:bad 0.01 0"); got != "-ERR (capacity should be larger than 0)\r\n" {
		t.Fatalf("BF.RESERVE capacity=0 = %q, want capacity error", got)
	}

	// BF.MADD creates the filter itself when the key is missing.
	autoKey := "TestBloomCommands:auto"
	if got := handle(t, "BF.MADD "+autoKey+" x"); got != "*1\r\n:1\r\n" {
		t.Fatalf("BF.MADD on missing key = %q, want [1]", got)
	}
	if got := handle(t, "BF.EXISTS "+autoKey+" x"); got != ":1\r\n" {
		t.Fatalf("BF.EXISTS after auto-create = %q, want :1", got)
	}
}

func TestCMSCommands(t *testing.T) {
	key := "TestCMSCommands:cms"

	// Unlike BF.MADD, a sketch is never created implicitly.
	if got := handle(t, "CMS.INCRBY "+key+" a 1"); got != "-CMS: key does not exist\r\n" {
		t.Fatalf("CMS.INCRBY on missing key = %q, want key-does-not-exist error", got)
	}
	if got := handle(t, "CMS.INITBYDIM "+key+" 200 5"); got != "+OK\r\n" {
		t.Fatalf("CMS.INITBYDIM = %q, want +OK", got)
	}
	if got := handle(t, "CMS.INITBYDIM "+key+" 200 5"); got != "-CMS: key already exists\r\n" {
		t.Fatalf("CMS.INITBYDIM twice = %q, want key-already-exists error", got)
	}
	if got := handle(t, "CMS.INCRBY "+key+" a 3 b 1"); got != "*2\r\n:3\r\n:1\r\n" {
		t.Fatalf("CMS.INCRBY = %q, want [3 1]", got)
	}
	if got := handle(t, "CMS.QUERY "+key+" a b missing"); got != "*3\r\n:3\r\n:1\r\n:0\r\n" {
		t.Fatalf("CMS.QUERY = %q, want [3 1 0]", got)
	}
	want := "*6\r\n$5\r\nwidth\r\n:200\r\n$5\r\ndepth\r\n:5\r\n$5\r\ncount\r\n:4\r\n"
	if got := handle(t, "CMS.INFO "+key); got != want {
		t.Fatalf("CMS.INFO = %q, want %q", got, want)
	}

	// INITBYPROB derives the same dimensions CalcCMSDim does.
	probKey := "TestCMSCommands:prob"
	if got := handle(t, "CMS.INITBYPROB "+probKey+" 0.001 0.01"); got != "+OK\r\n" {
		t.Fatalf("CMS.INITBYPROB = %q, want +OK", got)
	}
	wantProb := "*6\r\n$5\r\nwidth\r\n:2000\r\n$5\r\ndepth\r\n:7\r\n$5\r\ncount\r\n:0\r\n"
	if got := handle(t, "CMS.INFO "+probKey); got != wantProb {
		t.Fatalf("CMS.INFO after INITBYPROB = %q, want %q", got, wantProb)
	}
	if got := handle(t, "CMS.INCRBY "+key+" a"); got != "-CMS: wrong number of arguments for 'cms.incrby' command\r\n" {
		t.Fatalf("CMS.INCRBY odd args = %q, want arity error", got)
	}
}

func TestProbabilisticWrongType(t *testing.T) {
	key := "TestProbabilisticWrongType:key"
	handle(t, "SET "+key+" hello")

	for _, line := range []string{
		"BF.RESERVE " + key + " 0.01 100",
		"BF.MADD " + key + " a",
		"BF.EXISTS " + key + " a",
		"CMS.INITBYDIM " + key + " 100 5",
		"CMS.INCRBY " + key + " a 1",
		"CMS.QUERY " + key + " a",
		"CMS.INFO " + key,
	} {
		if got := handle(t, line); !strings.HasPrefix(got, "-WRONGTYPE") {
			t.Errorf("%s = %q, want WRONGTYPE error", line, got)
		}
	}

	// DEL must reach the bloom and CMS keyspaces too, or a stale filter
	// resurfaces after the key is supposedly gone.
	bfKey, cmsKey := "TestProbabilisticWrongType:bf", "TestProbabilisticWrongType:cms"
	handle(t, "BF.MADD "+bfKey+" a")
	handle(t, "CMS.INITBYDIM "+cmsKey+" 100 5")
	if got := handle(t, "EXISTS "+bfKey+" "+cmsKey); got != ":2\r\n" {
		t.Fatalf("EXISTS = %q, want :2", got)
	}
	if got := handle(t, "DEL "+bfKey+" "+cmsKey); got != ":2\r\n" {
		t.Fatalf("DEL = %q, want :2", got)
	}
	if got := handle(t, "EXISTS "+bfKey+" "+cmsKey); got != ":0\r\n" {
		t.Fatalf("EXISTS after DEL = %q, want :0", got)
	}
}
