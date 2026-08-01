// Lecture 2: bound the number of concurrent connection-handling goroutines
// with a semaphore-backed pool, instead of spawning one unboundedly per
// connection like lecture 1 does.
package main

import (
	"fmt"
	"net"
)

type GoroutinePool struct {
	semaphore chan struct{}
}

func (g *GoroutinePool) Get() {
	g.semaphore <- struct{}{}
}

func (g *GoroutinePool) Return() {
	<-g.semaphore
}

func handleConnection(c net.Conn) {
	defer c.Close()
	buffer := make([]byte, 1024)
	_, err := c.Read(buffer) // blocks until the client sends data
	if err != nil {
		fmt.Println("Error reading from connection:", err)
		return
	}

	fmt.Println(string(buffer))
	c.Write([]byte("Hello from low-level TCP server!\n"))
}

func main() {
	ln, err := net.Listen("tcp", ":3000")
	if err != nil {
		fmt.Println("listen error:", err)
		return
	}
	fmt.Println("Server started on port 3000")

	pool := &GoroutinePool{
		semaphore: make(chan struct{}, 1), // pool size = 1: handlers run one at a time
	}

	for {
		conn, err := ln.Accept()
		if err != nil {
			fmt.Println("Error accepting connection:", err)
			continue
		}
		fmt.Println("Connection accepted from", conn.RemoteAddr())
		pool.Get()
		go func() {
			defer pool.Return()
			handleConnection(conn)
		}()
	}
}
