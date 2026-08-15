// Lecture 3: speak real RESP and let a single connection send many
// commands in a row, still on a semaphore-bounded thread pool.
package main

import (
	"bufio"
	"fmt"
	"io"
	"net"

	"redis_k2/server/internal/command"
	"redis_k2/server/internal/protocol"
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
	reader := bufio.NewReader(c)
	for {
		args, err := protocol.ReadCommand(reader)
		if err != nil {
			if err != io.EOF {
				fmt.Println("read error:", err)
			}
			return
		}
		if len(args) == 0 {
			continue
		}
		if _, err := c.Write(command.Handle(args)); err != nil {
			fmt.Println("write error:", err)
			return
		}
	}
}

func main() {
	ln, err := net.Listen("tcp", ":3000")
	if err != nil {
		fmt.Println("listen error:", err)
		return
	}
	fmt.Println("Server started on port 3000")

	// sized to comfortably cover `redis-benchmark -c 500`
	pool := &GoroutinePool{
		semaphore: make(chan struct{}, 1024),
	}

	for {
		conn, err := ln.Accept()
		if err != nil {
			fmt.Println("Error accepting connection:", err)
			continue
		}
		pool.Get()
		go func() {
			defer pool.Return()
			handleConnection(conn)
		}()
	}
}
