// Package shutdown wires OS signals to graceful shutdown for anything that
// exposes a Close() error. It has no idea what io.Closer it's holding -
// that's the point: the event loop doesn't need to know signals exist, and
// this package doesn't need to know the event loop exists.
package shutdown

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
)

// WaitAndClose blocks until SIGINT or SIGTERM arrives, then closes c. Meant
// to be run in its own goroutine, e.g. `go shutdown.WaitAndClose(srv)`.
func WaitAndClose(c io.Closer) {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigs
	fmt.Println("received", sig, "- shutting down")
	_ = c.Close()
}
