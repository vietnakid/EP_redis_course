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
	_, err := c.Read(buffer) // blocked nếu như client chưa gửi data
	if err != nil {
		fmt.Println("Error reading from connection:", err)
		return
	}

	// async condition check buffer co data chưa
	fmt.Println(string(buffer))
	c.Write([]byte("Hello from low-level TCP server!\n"))
}

func main() {
	// create a listener for network tcp on prot 3000
	ln, err := net.Listen("tcp", ":3000")
	if err != nil {
		// handle error
		// OS 1 process listen 1 port at the same time.
		// Hardward thì thoải mái
	}
	fmt.Println("Server started on port 3000")

	semaphore := &GoroutinePool{
		semaphore: make(chan struct{}, 1), // semaphore size = 1
	}

	for { // blocked
		conn, err := ln.Accept()
		if err != nil {
			// handle error
			fmt.Println("Error accepting connection:", err)
			continue
		}
		fmt.Println("Connection accepted from ", conn.RemoteAddr())
		semaphore.Get()
		// client 11 blocked bởi dòng này
		go func() {
			handleConnection(conn) // 1 connection = 1 go-routine
			// vẫn tạo go-routine
			// go routine cũ 11 enter, (1) -> exit -> GCed
			semaphore.Return()
		}()
	}
}
