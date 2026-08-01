// Lecture 4: one thread, no connection pool - instead the OS multiplexer
// (epoll on Linux, kqueue on macOS) tells us which of the many open file
// descriptors are actually ready to read, and we serve every connection
// from a single event loop.
package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net"
	"syscall"

	"redis_k2/lecture4_io_multiplexing/internal/command"
	"redis_k2/lecture4_io_multiplexing/internal/protocol"
	"redis_k2/lecture4_io_multiplexing/io_multiplexing"
)

// handleReadable is called once per read-ready event on connFd. It does a
// single, non-blocking-in-spirit read of whatever is currently buffered by
// the kernel, then executes every RESP command found in that chunk. A
// command that straddles two reads is not reassembled - good enough for a
// lecture on event loops, not a fully spec-compliant server.
func handleReadable(connFd int) {
	buf := make([]byte, 4096)
	n, err := syscall.Read(connFd, buf)
	if err != nil || n == 0 {
		if err != nil && err != io.EOF {
			fmt.Println("read error:", err)
		}
		_ = syscall.Close(connFd)
		return
	}

	reader := bufio.NewReader(bytes.NewReader(buf[:n]))
	for {
		args, err := protocol.ReadCommand(reader)
		if err != nil {
			return
		}
		if len(args) == 0 {
			continue
		}
		reply := command.Handle(args)
		if _, err := syscall.Write(connFd, reply); err != nil {
			fmt.Println("write error:", err)
			_ = syscall.Close(connFd)
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
