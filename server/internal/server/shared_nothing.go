// shared_nothing.go implements Lecture 9-10's shared-nothing architecture:
// N Workers, each with a private *datastructure.Store and no lock, fed by
// N IOHandlers (each its own epoll/kqueue), fed in turn by one or more
// listener goroutines round-robin-handing off accepted connections. See
// server/BENCHMARK.md for what this buys over the single-threaded Server
// in internal/server/server.go, and when it doesn't.
package server

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// SharedNothingServer is the shared-nothing engine's equivalent of Server:
// New sets everything up, Run drives it until Close is called, from any
// goroutine - same external contract main.go already knows from the
// single-threaded engine.
type SharedNothingServer struct {
	workers    []*Worker
	ioHandlers []*IOHandler

	listenerFiles []*os.File
	listenerFds   []int
	// nextIOHandler round-robins accepted connections across ioHandlers.
	// It's atomic because every listener goroutine (there can be more than
	// one under -listeners=N) increments it concurrently - the only piece
	// of state in this engine that's actually shared across goroutines,
	// and it holds nothing but a counter.
	nextIOHandler atomic.Uint64

	closed atomic.Bool
	once   sync.Once
}

// NewSharedNothingServer builds numWorkers Workers and numIOHandlers
// IOHandlers, then binds numListeners listener sockets to addr - more than
// one only via SO_REUSEPORT (see createListener), letting the kernel
// spread new connections across them instead of one goroutine's Accept
// loop doing it alone.
func NewSharedNothingServer(addr string, numWorkers, numIOHandlers, numListeners int, simulateCPUWork time.Duration) (*SharedNothingServer, error) {
	m := &SharedNothingServer{}

	m.workers = make([]*Worker, numWorkers)
	for i := range m.workers {
		m.workers[i] = NewWorker(i, 1024, simulateCPUWork)
	}

	m.ioHandlers = make([]*IOHandler, numIOHandlers)
	for i := range m.ioHandlers {
		h, err := NewIOHandler(i, m.workers, &m.closed)
		if err != nil {
			return nil, fmt.Errorf("create io handler %d: %w", i, err)
		}
		m.ioHandlers[i] = h
	}

	reusePort := numListeners > 1
	for i := 0; i < numListeners; i++ {
		file, fd, err := createListener(addr, reusePort)
		if err != nil {
			return nil, fmt.Errorf("create listener %d: %w", i, err)
		}
		m.listenerFiles = append(m.listenerFiles, file)
		m.listenerFds = append(m.listenerFds, fd)
	}

	return m, nil
}

// createListener binds addr and returns the raw fd a listener goroutine
// can syscall.Accept on - the same fd-based style server.go's single
// listener already uses, just possibly with SO_REUSEPORT turned on first.
// The returned *os.File must be kept alive for as long as fd is used: it
// owns the duplicated descriptor net.Listener.File() hands back.
func createListener(addr string, reusePort bool) (*os.File, int, error) {
	var lc net.ListenConfig
	if reusePort {
		lc.Control = func(_, _ string, c syscall.RawConn) error {
			var sockoptErr error
			if err := c.Control(func(fd uintptr) {
				sockoptErr = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_REUSEPORT, 1)
			}); err != nil {
				return err
			}
			return sockoptErr
		}
	}
	ln, err := lc.Listen(context.Background(), "tcp", addr)
	if err != nil {
		return nil, -1, fmt.Errorf("listen: %w", err)
	}
	tcpListener := ln.(*net.TCPListener)
	file, err := tcpListener.File()
	if err != nil {
		return nil, -1, fmt.Errorf("get listener fd: %w", err)
	}
	return file, int(file.Fd()), nil
}

// Run starts every IOHandler's event loop and every listener's accept
// loop, then blocks until Close has been called and all of them have
// wound down.
func (m *SharedNothingServer) Run() error {
	var wg sync.WaitGroup

	for _, h := range m.ioHandlers {
		wg.Add(1)
		go func(h *IOHandler) {
			defer wg.Done()
			h.Run()
		}(h)
	}

	for i := range m.listenerFiles {
		wg.Add(1)
		go func(id, fd int, file *os.File) {
			defer wg.Done()
			m.runListener(id, fd, file)
		}(i, m.listenerFds[i], m.listenerFiles[i])
	}

	wg.Wait()
	log.Println("shutting down")
	return nil
}

// runListener blocks in syscall.Accept in a loop, handing every accepted
// connection off to an IOHandler round-robin. It never touches an
// IOHandler's internal state directly - only newConnCh - so nothing here
// needs a lock even with -listeners>1 running several of these at once.
func (m *SharedNothingServer) runListener(id, fd int, file *os.File) {
	defer file.Close()
	for {
		connFd, sa, err := syscall.Accept(fd)
		if err != nil {
			if m.closed.Load() {
				return
			}
			log.Println("listener", id, "accept error:", err)
			continue
		}
		addr := sockaddrString(sa)
		handlerID := int(m.nextIOHandler.Add(1)-1) % len(m.ioHandlers)
		m.ioHandlers[handlerID].newConnCh <- newConn{fd: connFd, addr: addr}
	}
}

// Close unblocks every listener's Accept and every IOHandler's Wait, then
// closes every Worker's TaskCh so each one drains whatever's buffered and
// exits on its own - the same Close-once, no-busy-loop shape
// server.go's Close already uses, just fanned out across every goroutine
// this engine started instead of just one.
func (m *SharedNothingServer) Close() error {
	m.once.Do(func() {
		m.closed.Store(true)
		for _, fd := range m.listenerFds {
			// shutdown, not just close: a listener goroutine blocked in
			// syscall.Accept on this fd is only guaranteed to unblock by
			// shutting the socket down first - closing it out from under
			// a blocked Accept is not portable.
			_ = syscall.Shutdown(fd, syscall.SHUT_RDWR)
		}
		for _, file := range m.listenerFiles {
			_ = file.Close()
		}
		for _, h := range m.ioHandlers {
			_ = h.mp.Close()
		}
		for _, w := range m.workers {
			close(w.TaskCh)
		}
	})
	return nil
}
