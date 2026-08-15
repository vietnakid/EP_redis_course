// Package protocol implements RESP (REdis Serialization Protocol) parsing
// and encoding. It is shared by every lecture's connection handler so the
// wire format stays identical no matter which I/O strategy reads it.
package protocol

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
)

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
