package main

import (
	"fmt"
	"log"
	"net"

	"github.com/gproxy/internal/tcpsplice"
)

const (
	PROXY_ADDR  = ":4000"
	SERVER_ADDR = ":5000"
)

func main() {
	pln, err := net.Listen("tcp", PROXY_ADDR)
	if err != nil {
		log.Fatalf("Failed to Listen on%s\n", PROXY_ADDR)
	}

	fmt.Printf("Server running on port%s \n", PROXY_ADDR)

	for {
		conn, err := pln.Accept()
		if err != nil {
			log.Printf("Can't accept connection %s\n", err.Error())
			return
		}

		go handleConn(conn)
	}
}

func handleConn(conn net.Conn) {
	fmt.Printf("New connection have been established %s AND forwarded to %s\n", conn.RemoteAddr().String(), SERVER_ADDR)
	defer conn.Close()

	serverConn, err := net.Dial("tcp", SERVER_ADDR)
	if err != nil {
		log.Fatalf("Cannot connect to server %s", err.Error())
	}
	defer serverConn.Close()

	// serverConn -> conn
	go tcpsplice.Transport(serverConn, conn)
	// go userspace.Transport(serverConn, conn)

	// conn -> serverConn
	tcpsplice.Transport(conn, serverConn)
	// userspace.Transport(conn, serverConn)
}
