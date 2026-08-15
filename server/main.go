// Lecture 5: same single-threaded epoll/kqueue event loop as lecture 4, but
// now speaking a complete-enough RESP protocol - SET with EX/PX expiration,
// TTL/PTTL, and commands that straddle multiple read() calls are correctly
// reassembled instead of dropped.
package main

import (
	"fmt"
	"io"
	"net"
	"syscall"

	"redis_k2/server/internal/command"
	"redis_k2/server/internal/protocol"
	"redis_k2/server/io_multiplexing"
)

// pending holds, per connection fd, whatever bytes have been read but not
// yet parsed into a full command. Only the single event-loop goroutine ever
// touches this map, so it needs no locking.
var pending = make(map[int][]byte)

// handleReadable is called once per read-ready event on connFd. It appends
// this read's bytes to whatever was left over from the previous event, then
// executes every complete RESP command it can parse out of the buffer. Any
// trailing partial command stays in pending until the next readable event.
func handleReadable(connFd int) {
	buf := make([]byte, 4096)
	n, err := syscall.Read(connFd, buf)
	if err != nil || n == 0 {
		if err != nil && err != io.EOF {
			fmt.Println("read error:", err)
		}
		closeConn(connFd)
		return
	}

	data := append(pending[connFd], buf[:n]...)
	for {
		args, consumed, err := protocol.ParseCommand(data)
		if err == protocol.ErrIncomplete {
			break
		}
		if err != nil {
			fmt.Println("protocol error:", err)
			closeConn(connFd)
			return
		}
		data = data[consumed:]
		if len(args) == 0 {
			continue
		}
		reply := command.Handle(args)
		if _, err := syscall.Write(connFd, reply); err != nil {
			fmt.Println("write error:", err)
			closeConn(connFd)
			return
		}
	}

	if len(data) == 0 {
		delete(pending, connFd)
	} else {
		leftover := make([]byte, len(data))
		copy(leftover, data)
		pending[connFd] = leftover
	}
}

func closeConn(connFd int) {
	_ = syscall.Close(connFd)
	delete(pending, connFd)
}

func main() {
	ln, err := net.Listen("tcp", ":3000")
	if err != nil {
		fmt.Println("listen error:", err)
		return
	}
	fmt.Println("Server started on port 3000")

	tcpListener := ln.(*net.TCPListener)
	listenerFile, err := tcpListener.File()
	if err != nil {
		fmt.Println("failed to get listener fd:", err)
		return
	}
	defer listenerFile.Close()
	serverFd := int(listenerFile.Fd())

	multiplexer, err := io_multiplexing.CreateIOMultiplexer()
	if err != nil {
		fmt.Println("failed to create io multiplexer:", err)
		return
	}
	defer multiplexer.Close()

	if err := multiplexer.Monitor(io_multiplexing.Event{Fd: serverFd, Op: io_multiplexing.OpRead}); err != nil {
		fmt.Println("failed to monitor listener fd:", err)
		return
	}

	for {
		events, err := multiplexer.Wait()
		if err != nil {
			fmt.Println("wait error:", err)
			continue
		}

		for _, event := range events {
			if event.Fd == serverFd {
				connFd, _, err := syscall.Accept(serverFd)
				if err != nil {
					fmt.Println("accept error:", err)
					continue
				}
				if err := multiplexer.Monitor(io_multiplexing.Event{Fd: connFd, Op: io_multiplexing.OpRead}); err != nil {
					fmt.Println("failed to monitor conn fd:", err)
					_ = syscall.Close(connFd)
				}
				continue
			}
			handleReadable(event.Fd)
		}
	}
}
