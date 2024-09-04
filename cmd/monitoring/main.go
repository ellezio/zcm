package main

import (
	"encoding/binary"
	"fmt"
	"log"
	"net"
)

func main() {

	l, _ := net.Listen("tcp", "0.0.0.0:10050")
	fmt.Println("Listening at :10050")
	for {
		conn, err := l.Accept()
		if err != nil {
			log.Fatal(err)
		}

		go connection(conn)
	}
}

func connection(conn net.Conn) {
	rAddr := conn.RemoteAddr()
	fmt.Println("New Connection ", rAddr)

	defer conn.Close()
	defer fmt.Printf("%s connection closed\n", rAddr)

	b := make([]byte, 4)
	n, err := conn.Read(b)
	if err != nil {
		fmt.Println("err: ", err)
		return
	}

	fmt.Printf("Code: %s\n", b)

	b = make([]byte, 1)
	n, err = conn.Read(b)
	if err != nil {
		fmt.Println("err: ", err)
		return
	}

	fmt.Println("Flag: ", b)

	b = make([]byte, 4)
	n, err = conn.Read(b)
	if err != nil {
		fmt.Println("err: ", err)
		return
	}

	dataLen := binary.LittleEndian.Uint32(b)
	fmt.Printf("Data length: %d\n", dataLen)

	b = make([]byte, 4)
	n, err = conn.Read(b)
	if err != nil {
		fmt.Println("err: ", err)
		return
	}

	b = make([]byte, dataLen)
	n, err = conn.Read(b)
	if err != nil {
		fmt.Println("err: ", err)
		return
	}

	fmt.Printf("Read %d bytes\n", n)
	fmt.Printf("Data: %s\n", b)
}
