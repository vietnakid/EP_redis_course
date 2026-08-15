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
	Wait() ([]Event, error)
	Close() error
}
