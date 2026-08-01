package main

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
)

type GoroutinePool struct {
	semaphore chan struct{}
}

func (g *GoroutinePool) Get() {
	g.semaphore <- struct{}{}
}

func (g *GoroutinePool) Return() {
	<-g.semaphore
}

var store = struct {
	sync.Mutex
	data map[string]string
}{data: make(map[string]string)}

var nilReply = []byte("$-1\r\n")

func encodeSimpleString(s string) []byte {
	return []byte("+" + s + "\r\n")
}

func encodeBulkString(s string) []byte {
	return []byte(fmt.Sprintf("$%d\r\n%s\r\n", len(s), s))
}

// readCommand parses one RESP array-of-bulk-strings request,
// e.g. *3\r\n$3\r\nSET\r\n$3\r\nfoo\r\n$3\r\nbar\r\n
func readCommand(r *bufio.Reader) ([]string, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return nil, err
	}
	line = strings.TrimRight(line, "\r\n")
	if len(line) == 0 {
		return nil, nil
	}
	if line[0] != '*' {
		// inline command: plain whitespace-separated args, e.g. "PING\r\n"
		return strings.Fields(line), nil
	}
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

func handleCommand(args []string) []byte {
	if len(args) == 0 {
		return nilReply
	}
	switch strings.ToUpper(args[0]) {
	case "PING":
		return encodeSimpleString("PONG")
	case "SET":
		if len(args) < 3 {
			return []byte("-ERR wrong number of arguments for 'SET'\r\n")
		}
		store.Lock()
		store.data[args[1]] = args[2]
		store.Unlock()
		return encodeSimpleString("OK")
	case "GET":
		if len(args) < 2 {
			return []byte("-ERR wrong number of arguments for 'GET'\r\n")
		}
		store.Lock()
		v, ok := store.data[args[1]]
		store.Unlock()
		if !ok {
			return nilReply
		}
		return encodeBulkString(v)
	default:
		return []byte("-ERR unknown command\r\n")
	}
}

func handleConnection(c net.Conn) {
	defer c.Close()
	reader := bufio.NewReader(c)
	for {
		args, err := readCommand(reader)
		if err != nil {
			if err != io.EOF {
				fmt.Println("read error:", err)
			}
			return
		}
		if len(args) == 0 {
			continue
		}
		if _, err := c.Write(handleCommand(args)); err != nil {
			fmt.Println("write error:", err)
			return
		}
	}
}

func main() {
	ln, err := net.Listen("tcp", ":3000")
	if err != nil {
		fmt.Println("listen error:", err)
		return
	}
	fmt.Println("Server started on port 3000")

	// sized to comfortably cover `redis-benchmark -c 500`
	pool := &GoroutinePool{
		semaphore: make(chan struct{}, 1024),
	}

	for {
		conn, err := ln.Accept()
		if err != nil {
			fmt.Println("Error accepting connection:", err)
			continue
		}
		pool.Get()
		go func() {
			defer pool.Return()
			handleConnection(conn)
		}()
	}
}
