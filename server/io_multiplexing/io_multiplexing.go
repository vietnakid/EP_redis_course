// Package io_multiplexing wraps the OS-level readiness API (epoll on Linux,
// kqueue on macOS) behind one small interface, so the event loop in lecture 4
// doesn't need to know which OS it's running on.
package io_multiplexing

// MaxConnection bounds how many ready events Wait can return in one call.
const MaxConnection = 1024

const OpRead = 0
const OpWrite = 1

type Operation uint32

type Event struct {
	Fd int
	Op Operation
}

type IOMultiplexer interface {
	Monitor(event Event) error
	// Wait blocks for I/O readiness up to timeoutMs milliseconds (negative
	// means block indefinitely). A timeout returns a nil/empty slice with a
	// nil error - callers use that to run periodic, non-I/O work (like the
	// active expiry sweep) on the same goroutine as the event loop, without
	// spawning a background ticker.
	Wait(timeoutMs int) ([]Event, error)
	Close() error
}
