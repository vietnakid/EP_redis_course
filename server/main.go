// Lecture 1: the simplest possible TCP server.
//
// One goroutine per accepted connection, no pooling, no protocol - just
// prove we can accept a connection, read a line, and write a reply.
package main

import (
	"fmt"
	"net"
)

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

	for {
		conn, err := ln.Accept()
		if err != nil {
			fmt.Println("Error accepting connection:", err)
			continue
		}
		fmt.Println("Connection accepted from", conn.RemoteAddr())
		go handleConnection(conn) // 1 connection = 1 goroutine, unbounded
	}
}
