// Lecture 9: same single-threaded epoll/kqueue event loop as lecture 7, now
// split into internal/server (listener + event loop + connections) and
// internal/shutdown (SIGINT/SIGTERM -> Close()), so a graceful shutdown is
// two goroutines that never share state: shutdown only ever touches the
// listener fd and the multiplexer via Server.Close, while internal/server's
// own goroutine keeps sole ownership of pending/connFds throughout.
package main

import (
	"log"
	"os"

	"redis_k2/server/internal/server"
	"redis_k2/server/internal/shutdown"
)

func main() {
	// Ldate|Ltime|Lmicroseconds for a real timestamp, Lshortfile so every
	// log line names the file:line it came from - stdlib covers both, no
	// logging library needed for a teaching server.
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds | log.Lshortfile)

	srv, err := server.New(":3000")
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("Server started on port 3000, pid=%d", os.Getpid())

	go shutdown.WaitAndClose(srv)

	if err := srv.Run(); err != nil {
		log.Println(err)
	}
}
