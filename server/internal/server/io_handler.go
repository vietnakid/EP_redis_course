package server

import (
	"hash/fnv"
	"io"
	"log"
	"math/rand"
	"sync/atomic"
	"syscall"
	"time"

	"redis_k2/server/internal/protocol"
	"redis_k2/server/io_multiplexing"
)

// ioHandlerPollInterval bounds IOHandler.Run's mp.Wait() call, so a
// connection handed off over newConnCh gets picked up and monitored
// promptly instead of only after the next unrelated I/O event.
const ioHandlerPollInterval = 100 * time.Millisecond

// newConn is one accepted connection handed from a listener goroutine to
// whichever IOHandler it was round-robined to.
type newConn struct {
	fd   int
	addr string
}

// IOHandler is one epoll/kqueue event loop - the shared-nothing engine's
// answer to "one Server" in the single-threaded design, just multiplied by
// numIOHandlers and talking to Workers instead of a Store directly.
//
// Every field below is touched only by this IOHandler's own Run goroutine,
// same single-writer discipline the single-threaded Server uses for
// pending/connFds - new connections arrive over newConnCh instead of a
// shared map, which is what lets every IOHandler skip the mutex the
// reference implementation needed.
type IOHandler struct {
	id int
	mp io_multiplexing.IOMultiplexer

	newConnCh chan newConn

	pending    map[int][]byte
	remoteAddr map[int]string

	workers []*Worker

	// closed is shared with the owning SharedNothingServer: Close sets it
	// before closing mp, so Run can tell a Close-triggered Wait error
	// (expected, shut down cleanly) from a real one (log it).
	closed *atomic.Bool
}

func NewIOHandler(id int, workers []*Worker, closed *atomic.Bool) (*IOHandler, error) {
	mp, err := io_multiplexing.CreateIOMultiplexer()
	if err != nil {
		return nil, err
	}
	return &IOHandler{
		id:         id,
		mp:         mp,
		newConnCh:  make(chan newConn, 128),
		pending:    make(map[int][]byte),
		remoteAddr: make(map[int]string),
		workers:    workers,
		closed:     closed,
	}, nil
}

// Run drives this IOHandler's event loop until its multiplexer is closed
// (by the owning SharedNothingServer's Close).
func (h *IOHandler) Run() {
	for {
		events, err := h.mp.Wait(int(ioHandlerPollInterval / time.Millisecond))
		if err != nil {
			if h.closed.Load() {
				h.closeAllConns()
				return
			}
			if err == syscall.EINTR {
				continue
			}
			log.Println("io handler", h.id, "wait error:", err)
			continue
		}

		for _, event := range events {
			h.handleReadable(event.Fd)
		}

		h.drainNewConns()
	}
}

// drainNewConns picks up every connection the listener(s) have handed off
// since the last pass, without blocking - ioHandlerPollInterval already
// bounds how long a freshly accepted connection waits before this runs.
func (h *IOHandler) drainNewConns() {
	for {
		select {
		case nc := <-h.newConnCh:
			if err := h.mp.Monitor(io_multiplexing.Event{Fd: nc.fd, Op: io_multiplexing.OpRead}); err != nil {
				log.Println("io handler", h.id, "failed to monitor conn fd:", err)
				_ = syscall.Close(nc.fd)
				continue
			}
			h.remoteAddr[nc.fd] = nc.addr
			log.Printf("io handler %d monitoring fd=%d from %s", h.id, nc.fd, nc.addr)
		default:
			return
		}
	}
}

// handleReadable mirrors the single-threaded Server's method of the same
// name, with one difference: instead of calling command.Handle directly,
// it hashes the key out of the parsed command and dispatches to whichever
// Worker owns that partition.
func (h *IOHandler) handleReadable(connFd int) {
	buf := make([]byte, 4096)
	n, err := syscall.Read(connFd, buf)
	if err != nil || n == 0 {
		if err != nil && err != io.EOF {
			log.Println("io handler", h.id, "read error:", err)
		}
		h.closeConn(connFd)
		return
	}

	data := append(h.pending[connFd], buf[:n]...)
	for {
		cmd, consumed, err := protocol.ParseCommand(data)
		if err == protocol.ErrIncomplete {
			break
		}
		if err != nil {
			log.Println("io handler", h.id, "protocol error:", err)
			h.closeConn(connFd)
			return
		}
		data = data[consumed:]
		if cmd.Name == "" {
			continue
		}

		reply := h.dispatch(cmd)

		if _, err := syscall.Write(connFd, reply); err != nil {
			log.Println("io handler", h.id, "write error:", err)
			h.closeConn(connFd)
			return
		}
	}

	if len(data) == 0 {
		delete(h.pending, connFd)
	} else {
		leftover := make([]byte, len(data))
		copy(leftover, data)
		h.pending[connFd] = leftover
	}
}

// getPartitionID picks a deterministic worker for key: same key, same
// worker, every call - what makes SET then GET on the same key land on the
// Store that actually holds it.
func getPartitionID(key string, numWorkers int) int {
	hasher := fnv.New32a()
	hasher.Write([]byte(key))
	return int(hasher.Sum32() % uint32(numWorkers))
}

// dispatch routes cmd to the Worker owning its key's partition (or a
// random Worker for a keyless command like PING/INFO - there's nothing to
// hash), then blocks for that Worker's reply. Blocking here is deliberate:
// it's what keeps one IOHandler's view of "handle this event, then the
// next" simple, exactly as the lecture describes it - other IOHandlers and
// Workers keep making progress independently in the meantime.
func (h *IOHandler) dispatch(cmd protocol.Command) []byte {
	var workerID int
	if len(cmd.Args) > 0 {
		workerID = getPartitionID(cmd.Args[0], len(h.workers))
	} else {
		workerID = rand.Intn(len(h.workers))
	}
	replyCh := make(chan []byte, 1)
	h.workers[workerID].TaskCh <- &Task{Cmd: cmd, ReplyCh: replyCh}
	return <-replyCh
}

func (h *IOHandler) closeConn(connFd int) {
	log.Printf("io handler %d closing fd=%d addr=%s", h.id, connFd, h.remoteAddr[connFd])
	_ = syscall.Close(connFd)
	delete(h.pending, connFd)
	delete(h.remoteAddr, connFd)
}

func (h *IOHandler) closeAllConns() {
	for fd := range h.remoteAddr {
		h.closeConn(fd)
	}
}
