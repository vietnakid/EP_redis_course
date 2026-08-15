// Package protocol implements RESP (REdis Serialization Protocol) parsing
// and encoding. It is shared by every lecture's connection handler so the
// wire format stays identical no matter which I/O strategy reads it.
package protocol

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ErrIncomplete means buf does not yet contain a full command. The caller
// must not consume any bytes and should retry once more data has arrived.
// The event-loop lecture needs this: unlike a blocking bufio.Reader over a
// socket, a single non-blocking read may hand us half a command.
var ErrIncomplete = errors.New("protocol: incomplete command")

// NilReply is the RESP2 encoding of a null bulk string - what GET (and
// friends) reply with when a key doesn't exist.
var NilReply = []byte("$-1\r\n")

// Value is anything Encode knows how to serialize as one RESP2 reply.
// Handlers build replies out of plain Go values instead of hand-assembling
// wire bytes, so a new RESP-shaped reply is a new case in Encode's type
// switch, not a new Encode<Type> function every caller has to learn and
// every call site has to migrate to.
//
// Supported concrete types cover RESP2's five basic types
// (https://redis.io/docs/latest/develop/reference/protocol-spec/):
//
//	nil            -> Null Bulk String   ($-1\r\n)
//	SimpleString   -> Simple String      (+OK\r\n)
//	error          -> Simple Error       (-ERR ...\r\n)
//	string         -> Bulk String        ($3\r\nfoo\r\n)
//	int64          -> Integer            (:123\r\n)
//	[]Value        -> Array, each element encoded recursively (*N\r\n...)
//
// RESP3-only types (maps, sets, doubles, booleans, big numbers, verbatim
// strings, push messages, attributes) are intentionally out of scope.
type Value interface{}

// SimpleString marks a string that must be encoded as RESP's "+" simple
// string instead of the "$" bulk string a plain Go string gets. Real
// Redis reserves simple strings for a handful of canonical replies (OK,
// PONG); everything else is bulk.
type SimpleString string

// Encode serializes v as one complete RESP2 reply. An unsupported Go type
// is a programmer error, not something a client can trigger, so it panics
// rather than silently emitting a malformed reply.
func Encode(v Value) []byte {
	switch x := v.(type) {
	case nil:
		return NilReply
	case SimpleString:
		return []byte("+" + string(x) + "\r\n")
	case error:
		return []byte("-" + x.Error() + "\r\n")
	case string:
		return []byte(fmt.Sprintf("$%d\r\n%s\r\n", len(x), x))
	case int64:
		return []byte(fmt.Sprintf(":%d\r\n", x))
	case []Value:
		var buf bytes.Buffer
		fmt.Fprintf(&buf, "*%d\r\n", len(x))
		for _, elem := range x {
			buf.Write(Encode(elem))
		}
		return buf.Bytes()
	default:
		panic(fmt.Sprintf("protocol: Encode: unsupported reply type %T", v))
	}
}

// Thin, named sugar over Encode for the handful of reply shapes every
// command needs constantly - each is one line because the real work
// (understanding the RESP wire format, handling nesting) lives once in
// Encode, not repeated per type.
func EncodeSimpleString(s string) []byte { return Encode(SimpleString(s)) }
func EncodeBulkString(s string) []byte   { return Encode(s) }
func EncodeError(msg string) []byte      { return Encode(errors.New(msg)) }
func EncodeInteger(n int64) []byte       { return Encode(n) }

// Command is one fully-parsed client request: the verb (e.g. "SET") split
// out from its arguments, instead of a flat []string where the verb sits
// unlabeled at index 0. Handlers switch on Name and index Args
// positionally without needing to remember to skip the first element.
// Name == "" represents "no command" (a blank inline line, or an empty
// multibulk array) - there was nothing to dispatch.
type Command struct {
	Name string
	Args []string
}

// ParseCommand tries to parse one request off the front of buf. It
// accepts both the RESP multibulk form (e.g.
// *3\r\n$3\r\nSET\r\n$3\r\nfoo\r\n$3\r\nbar\r\n) and the inline form (e.g.
// "PING\r\n"), since redis-benchmark's ping test sends both. It works
// directly on an accumulated byte slice instead of blocking on a reader,
// so it can report "not enough data yet" (ErrIncomplete) instead of
// erroring out. On success it returns how many bytes of buf were
// consumed; the caller must drop exactly that many bytes before parsing
// the next command out of what remains.
func ParseCommand(buf []byte) (cmd Command, consumed int, err error) {
	line, next, ok := readLine(buf, 0)
	if !ok {
		return Command{}, 0, ErrIncomplete
	}
	if len(line) == 0 {
		return Command{}, next, nil
	}
	if line[0] != '*' {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			return Command{}, next, nil
		}
		return Command{Name: fields[0], Args: fields[1:]}, next, nil
	}
	return parseMultiBulk(buf, line, next)
}

func parseMultiBulk(buf []byte, header string, pos int) (Command, int, error) {
	n, err := strconv.Atoi(header[1:])
	if err != nil {
		return Command{}, 0, fmt.Errorf("protocol error: invalid multibulk length %q", header)
	}
	if n <= 0 {
		return Command{}, pos, nil
	}

	tokens := make([]string, n)
	for i := 0; i < n; i++ {
		typeLine, next, ok := readLine(buf, pos)
		if !ok {
			return Command{}, 0, ErrIncomplete
		}
		if len(typeLine) == 0 || typeLine[0] != '$' {
			return Command{}, 0, fmt.Errorf("protocol error: expected '$', got %q", typeLine)
		}
		size, err := strconv.Atoi(typeLine[1:])
		if err != nil || size < 0 {
			return Command{}, 0, fmt.Errorf("protocol error: invalid bulk length %q", typeLine)
		}
		pos = next

		end := pos + size
		if end+2 > len(buf) {
			return Command{}, 0, ErrIncomplete
		}
		if buf[end] != '\r' || buf[end+1] != '\n' {
			return Command{}, 0, fmt.Errorf("protocol error: expected CRLF after bulk payload")
		}
		tokens[i] = string(buf[pos:end])
		pos = end + 2
	}
	return Command{Name: tokens[0], Args: tokens[1:]}, pos, nil
}

// readLine finds the "\n"-terminated line starting at start (an optional
// trailing '\r' is stripped). It returns the line content, the offset
// right after the terminator, and whether a full line was found at all.
func readLine(buf []byte, start int) (string, int, bool) {
	idx := bytes.IndexByte(buf[start:], '\n')
	if idx < 0 {
		return "", 0, false
	}
	end := start + idx
	line := bytes.TrimSuffix(buf[start:end], []byte("\r"))
	return string(line), end + 1, true
}
