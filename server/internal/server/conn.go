package server

import (
	"io"
	"log"
	"syscall"

	"redis_k2/server/internal/command"
	"redis_k2/server/internal/protocol"
)

// handleReadable is called once per read-ready event on connFd. It appends
// this read's bytes to whatever was left over from the previous event, then
// executes every complete RESP command it can parse out of the buffer. Any
// trailing partial command stays in pending until the next readable event.
func (s *Server) handleReadable(connFd int) {
	buf := make([]byte, 4096)
	n, err := syscall.Read(connFd, buf)
	if err != nil || n == 0 {
		if err != nil && err != io.EOF {
			log.Println("read error:", err)
		}
		s.closeConn(connFd)
		return
	}

	data := append(s.pending[connFd], buf[:n]...)
	for {
		cmd, consumed, err := protocol.ParseCommand(data)
		if err == protocol.ErrIncomplete {
			break
		}
		if err != nil {
			log.Println("protocol error:", err)
			s.closeConn(connFd)
			return
		}
		data = data[consumed:]
		if cmd.Name == "" {
			continue
		}

		// handle that request
		simulateCPUWorkOnGet(cmd, s.simulateCPUWork)
		reply := command.Handle(s.store, cmd)

		if _, err := syscall.Write(connFd, reply); err != nil {
			log.Println("write error:", err)
			s.closeConn(connFd)
			return
		}
	}

	if len(data) == 0 {
		delete(s.pending, connFd)
	} else {
		leftover := make([]byte, len(data))
		copy(leftover, data)
		s.pending[connFd] = leftover
	}
}

func (s *Server) closeConn(connFd int) {
	log.Printf("closing fd=%d addr=%s", connFd, s.remoteAddr[connFd])
	_ = syscall.Close(connFd)
	delete(s.pending, connFd)
	delete(s.connFds, connFd)
	delete(s.remoteAddr, connFd)
}

// closeAllConns runs once, from Run's own goroutine, after it observes
// Close was called - see Close's comment for why connFds cleanup stays
// here instead of happening in Close itself.
func (s *Server) closeAllConns() {
	for fd := range s.connFds {
		s.closeConn(fd)
	}
}
