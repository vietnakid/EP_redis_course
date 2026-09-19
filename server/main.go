// Lecture 7: same single-threaded epoll/kqueue event loop as lecture 6, now
// with two probabilistic structures alongside the exact ones - a Bloom
// filter (BF.RESERVE/BF.MADD/BF.EXISTS) that answers set membership in a
// fixed bit array, and a Count-Min Sketch (CMS.INITBYDIM/CMS.INITBYPROB/
// CMS.INCRBY/CMS.QUERY/CMS.INFO) that estimates frequencies in a fixed
// counter grid. Both trade exactness for memory that never grows with the
// number of items stored.
package main

import (
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"redis_k2/server/internal/command"
	"redis_k2/server/internal/datastructure"
	"redis_k2/server/internal/protocol"
	"redis_k2/server/io_multiplexing"
)

// pending holds, per connection fd, whatever bytes have been read but not
// yet parsed into a full command. Only the single event-loop goroutine ever
// touches this map, so it needs no locking.
var pending = make(map[int][]byte)

// connFds tracks every currently-open client fd, purely so a graceful
// shutdown can close them all. Same single-goroutine ownership as pending.
var connFds = make(map[int]struct{})

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
		cmd, consumed, err := protocol.ParseCommand(data)
		if err == protocol.ErrIncomplete {
			break
		}
		if err != nil {
			fmt.Println("protocol error:", err)
			closeConn(connFd)
			return
		}
		data = data[consumed:]
		if cmd.Name == "" {
			continue
		}

		// handle that request
		reply := command.Handle(cmd)

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
	delete(connFds, connFd)
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

	// activeExpireInterval bounds multiplexer.Wait() so the loop wakes up
	// on its own even when every connection is idle, and runs the active
	// expiry sweep right here - same goroutine, same iteration, no ticker.
	const activeExpireInterval = 100 * time.Millisecond
	lastActiveExpire := time.Now()

	// SIGINT/SIGTERM just need to reach this one goroutine - the event loop
	// already wakes up every activeExpireInterval (see Wait's timeout
	// below), so checking a channel non-blockingly at the top of each
	// iteration bounds shutdown latency to that same ~100ms without a
	// ticker, a busy-loop, or any cross-goroutine state machine. Unlike a
	// multi-goroutine server, this loop is the only thing touching pending/
	// connFds/the multiplexer, so there's no "wait until idle" handshake to
	// get right - the signal is simply observed between two iterations that
	// were already going to happen.
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, syscall.SIGINT, syscall.SIGTERM)

	for {
		select {
		case sig := <-shutdown:
			fmt.Println("received", sig, "- shutting down")
			for fd := range connFds {
				closeConn(fd)
			}
			return
		default:
		}

		events, err := multiplexer.Wait(int(activeExpireInterval / time.Millisecond))
		if err != nil {
			// EINTR: Go's runtime async-preempts a hot goroutine with SIGURG,
			// which interrupts a blocked epoll_wait/kevent syscall. Benign
			// under load (e.g. redis-benchmark) - just retry, don't log.
			if err == syscall.EINTR {
				continue
			}
			fmt.Println("wait error: ", err)
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
					continue
				}
				connFds[connFd] = struct{}{}
				continue
			}

			// handle có event mới cho fd (có data mới hoặc có thể là fd đóng)
			handleReadable(event.Fd)
		}

		// don't use time.sleep() here, because it is blocking function()
		if time.Since(lastActiveExpire) >= activeExpireInterval {
			datastructure.ActiveExpireCycle()
			lastActiveExpire = time.Now()
		}
	}
}
