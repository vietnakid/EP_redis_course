// Package protocol implements RESP (REdis Serialization Protocol) parsing
// and encoding. It is shared by every lecture's connection handler so the
// wire format stays identical no matter which I/O strategy reads it.
package protocol

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// ErrIncomplete means buf does not yet contain a full command. The caller
// must not consume any bytes and should retry once more data has arrived.
// The event-loop lecture needs this: unlike a blocking bufio.Reader over a
// socket, a single non-blocking read may hand us half a command.
var ErrIncomplete = errors.New("protocol: incomplete command")

var NilReply = []byte("$-1\r\n")

func EncodeSimpleString(s string) []byte {
	return []byte("+" + s + "\r\n")
}

func EncodeBulkString(s string) []byte {
	return []byte(fmt.Sprintf("$%d\r\n%s\r\n", len(s), s))
}

func EncodeError(msg string) []byte {
	return []byte("-" + msg + "\r\n")
}

func EncodeInteger(n int64) []byte {
	return []byte(fmt.Sprintf(":%d\r\n", n))
}

// ReadCommand parses one request off r. It accepts both the RESP
// multibulk form (e.g. *3\r\n$3\r\nSET\r\n$3\r\nfoo\r\n$3\r\nbar\r\n) and the
// inline form (e.g. "PING\r\n"), since redis-benchmark's ping test sends both.
func ReadCommand(r *bufio.Reader) ([]string, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return nil, err
	}
	line = strings.TrimRight(line, "\r\n")
	if len(line) == 0 {
		return nil, nil
	}
	if line[0] != '*' {
		return strings.Fields(line), nil
	}
	return readMultiBulk(r, line)
}

func readMultiBulk(r *bufio.Reader, line string) ([]string, error) {
	n, err := strconv.Atoi(line[1:])
	if err != nil {
		return nil, err
	}

	args := make([]string, n)
	for i := 0; i < n; i++ {
		typeLine, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		typeLine = strings.TrimRight(typeLine, "\r\n")
		if len(typeLine) == 0 || typeLine[0] != '$' {
			return nil, fmt.Errorf("protocol error: expected '$', got %q", typeLine)
		}
		size, err := strconv.Atoi(typeLine[1:])
		if err != nil {
			return nil, err
		}
		buf := make([]byte, size+2) // payload + trailing \r\n
		if _, err := io.ReadFull(r, buf); err != nil {
			return nil, err
		}
		args[i] = string(buf[:size])
	}
	return args, nil
}

// ParseCommand tries to parse one request off the front of buf. Same wire
// format as ReadCommand, but works directly on an accumulated byte slice
// instead of blocking on a reader, so it can report "not enough data yet"
// instead of erroring out. On success it returns how many bytes of buf
// were consumed; the caller must drop exactly that many bytes before
// parsing the next command out of what remains.
func ParseCommand(buf []byte) (args []string, consumed int, err error) {
	line, next, ok := readLine(buf, 0)
	if !ok {
		return nil, 0, ErrIncomplete
	}
	if len(line) == 0 {
		return []string{}, next, nil
	}
	if line[0] != '*' {
		return strings.Fields(line), next, nil
	}
	return parseMultiBulk(buf, line, next)
}

func parseMultiBulk(buf []byte, header string, pos int) ([]string, int, error) {
	n, err := strconv.Atoi(header[1:])
	if err != nil {
		return nil, 0, fmt.Errorf("protocol error: invalid multibulk length %q", header)
	}
	if n <= 0 {
		return []string{}, pos, nil
	}

	args := make([]string, n)
	for i := 0; i < n; i++ {
		typeLine, next, ok := readLine(buf, pos)
		if !ok {
			return nil, 0, ErrIncomplete
		}
		if len(typeLine) == 0 || typeLine[0] != '$' {
			return nil, 0, fmt.Errorf("protocol error: expected '$', got %q", typeLine)
		}
		size, err := strconv.Atoi(typeLine[1:])
		if err != nil || size < 0 {
			return nil, 0, fmt.Errorf("protocol error: invalid bulk length %q", typeLine)
		}
		pos = next

		end := pos + size
		if end+2 > len(buf) {
			return nil, 0, ErrIncomplete
		}
		if buf[end] != '\r' || buf[end+1] != '\n' {
			return nil, 0, fmt.Errorf("protocol error: expected CRLF after bulk payload")
		}
		args[i] = string(buf[pos:end])
		pos = end + 2
	}
	return args, pos, nil
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
