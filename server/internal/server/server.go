// Package server owns the TCP listener, the I/O-multiplexed event loop, and
// every open client connection. It exposes just enough surface - New, Run,
// Close - for main to wire in signal handling (see internal/shutdown)
// without either goroutine reaching into the other's state.
package server

import (
	"fmt"
	"log"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"redis_k2/server/internal/datastructure"
	"redis_k2/server/io_multiplexing"
)

// activeExpireInterval bounds Wait() so the loop wakes up on its own even
// when every connection is idle, and runs the active expiry sweep right
// here - same goroutine, same iteration, no ticker.
const activeExpireInterval = 100 * time.Millisecond

// Server owns the listener fd, the multiplexer, and every per-connection
// buffer. Only the goroutine running Run ever touches pending/connFds -
// Close (meant to be called from another goroutine, e.g. a signal handler)
// only ever touches the listener fd and the multiplexer, both safe to close
// cross-goroutine. See Close's comment for why that split matters.
type Server struct {
	listenerFile *os.File
	listenerFd   int
	mp           io_multiplexing.IOMultiplexer

	pending map[int][]byte
	connFds map[int]struct{}
	// remoteAddr labels each connFd with "ip:port" purely for logging -
	// teaching aid so a close log line reads as more than a bare number.
	remoteAddr map[int]string

	// store is this Server's own keyspace - single-threaded, so one Store
	// is all it ever needs (contrast the shared-nothing engine, where each
	// Worker gets its own).
	store *datastructure.Store

	closed atomic.Bool
	once   sync.Once
}

func New(addr string) (*Server, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("listen: %w", err)
	}
	tcpListener := ln.(*net.TCPListener)
	listenerFile, err := tcpListener.File()
	if err != nil {
		return nil, fmt.Errorf("get listener fd: %w", err)
	}
	serverFd := int(listenerFile.Fd())

	mp, err := io_multiplexing.CreateIOMultiplexer()
	if err != nil {
		_ = listenerFile.Close()
		return nil, fmt.Errorf("create io multiplexer: %w", err)
	}
	if err := mp.Monitor(io_multiplexing.Event{Fd: serverFd, Op: io_multiplexing.OpRead}); err != nil {
		_ = mp.Close()
		_ = listenerFile.Close()
		return nil, fmt.Errorf("monitor listener fd: %w", err)
	}

	return &Server{
		listenerFile: listenerFile,
		listenerFd:   serverFd,
		mp:           mp,
		pending:      make(map[int][]byte),
		connFds:      make(map[int]struct{}),
		remoteAddr:   make(map[int]string),
		store:        datastructure.NewStore(),
	}, nil
}

// Close unblocks a Run loop that's blocked in mp.Wait(): closing the
// epoll/kqueue fd makes a pending Wait return immediately with an error -
// a standard trick on both epoll and kqueue, and why shutdown here doesn't
// need to poll a channel every iteration. It deliberately does NOT touch
// pending/connFds itself - those stay single-writer, cleaned up by Run's
// own goroutine once it observes closed()==true. Safe to call more than
// once, and safe to call concurrently with Run.
func (s *Server) Close() error {
	s.once.Do(func() {
		s.closed.Store(true)
		_ = s.mp.Close()
		_ = s.listenerFile.Close()
	})
	return nil
}

// Run drives the event loop until Close is called, from any goroutine.
func (s *Server) Run() error {
	lastActiveExpire := time.Now()

	for {
		events, err := s.mp.Wait(int(activeExpireInterval / time.Millisecond))
		if err != nil {
			if s.closed.Load() {
				log.Println("shutting down")
				s.closeAllConns()
				return nil
			}
			// EINTR: Go's runtime async-preempts a hot goroutine with
			// SIGURG, which interrupts a blocked epoll_wait/kevent
			// syscall. Benign under load (e.g. redis-benchmark) - just
			// retry, don't log.
			if err == syscall.EINTR {
				continue
			}
			log.Println("wait error:", err)
			continue
		}

		for _, event := range events {
			if event.Fd == s.listenerFd {
				s.acceptConn()
				continue
			}
			s.handleReadable(event.Fd)
		}

		// don't use time.sleep() here, because it is a blocking function
		if time.Since(lastActiveExpire) >= activeExpireInterval {
			s.store.ActiveExpireCycle()
			lastActiveExpire = time.Now()
		}
	}
}

func (s *Server) acceptConn() {
	connFd, sa, err := syscall.Accept(s.listenerFd)
	if err != nil {
		if !s.closed.Load() {
			log.Println("accept error:", err)
		}
		return
	}
	addr := sockaddrString(sa)
	if err := s.mp.Monitor(io_multiplexing.Event{Fd: connFd, Op: io_multiplexing.OpRead}); err != nil {
		log.Println("failed to monitor conn fd:", err)
		_ = syscall.Close(connFd)
		return
	}
	s.connFds[connFd] = struct{}{}
	s.remoteAddr[connFd] = addr
	log.Printf("accepted fd=%d from %s", connFd, addr)
}

// sockaddrString formats a syscall.Sockaddr as "ip:port" for logging. A
// dual-stack listener hands back IPv4 clients as v6-mapped addresses, so
// this unwraps those back to plain dotted-quad instead of logging them
// wrapped in brackets.
func sockaddrString(sa syscall.Sockaddr) string {
	switch a := sa.(type) {
	case *syscall.SockaddrInet4:
		return fmt.Sprintf("%s:%d", net.IP(a.Addr[:]).String(), a.Port)
	case *syscall.SockaddrInet6:
		ip := net.IP(a.Addr[:])
		if v4 := ip.To4(); v4 != nil {
			return fmt.Sprintf("%s:%d", v4.String(), a.Port)
		}
		return fmt.Sprintf("[%s]:%d", ip.String(), a.Port)
	default:
		return "unknown"
	}
}
