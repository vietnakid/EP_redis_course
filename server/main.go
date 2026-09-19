// Lecture 9: same single-threaded epoll/kqueue event loop as lecture 7, now
// split into internal/server (listener + event loop + connections) and
// internal/shutdown (SIGINT/SIGTERM -> Close()), so a graceful shutdown is
// two goroutines that never share state: shutdown only ever touches the
// listener fd and the multiplexer via Server.Close, while internal/server's
// own goroutine keeps sole ownership of pending/connFds throughout.
package main

import (
	"fmt"

	"redis_k2/server/internal/server"
	"redis_k2/server/internal/shutdown"
)

func main() {
	srv, err := server.New(":3000")
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println("Server started on port 3000")

	go shutdown.WaitAndClose(srv)

	if err := srv.Run(); err != nil {
		fmt.Println(err)
	}
}
