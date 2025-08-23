package main

import (
	"fmt"
	"log"
	"net"

	"github.com/gproxy/internal/sockmap"
	"github.com/gproxy/internal/tcpsplice"
	"github.com/gproxy/internal/userspace"
)

type TransportName string

const (
	PROXY_ADDR  = ":4000"
	SERVER_ADDR = ":5000"

	TCPSplice TransportName = "tcpSplice"
	Userland  TransportName = "userspace"
	Sockmap   TransportName = "sockmap"
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
	go getTransport(Sockmap)(serverConn, conn)

	// conn -> serverConn
	getTransport(Sockmap)(conn, serverConn)
}

func getTransport(name TransportName) func(src, dst net.Conn) {
	switch name {
	case TCPSplice:
		return tcpsplice.Transport
	case Userland:
		return userspace.Transport
	case Sockmap:
		return sockmap.Transport
	default:
		return userspace.Transport
	}
}
