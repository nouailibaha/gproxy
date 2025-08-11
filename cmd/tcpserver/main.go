package main

import (
	"fmt"
	"log"
	"net"
)

func main() {
	ln, err := net.Listen("tcp", ":5000")

	if err != nil {
		fmt.Println("Failed to lunch a TCP server")
	}

	fmt.Printf("Server running on port:%s \n", "5000")
	for {
		conn, err := ln.Accept()
		if err != nil {
			fmt.Println("Error when accepting conn.")
		}

		go handleConn(conn)
	}
}

func handleConn(conn net.Conn) {
	defer conn.Close()

	buff := make([]byte, 1024)
	for {
		n, err := conn.Read(buff)

		if err != nil {
			log.Println("Read error:", err)
			return
		}
		rsp := string(buff[:n])

		fmt.Printf("RCV: %s", rsp)

		ackToConnection(conn, rsp)
	}
}

func ackToConnection(conn net.Conn, rsp string) {
	if _, err := fmt.Fprintf(conn, "ACK: %s", rsp); err != nil {
		log.Fatal("Cant write to conn")
	}
}
