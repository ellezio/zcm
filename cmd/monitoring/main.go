package main

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
)

const (
	PROTOCOL      = "ZBXD"
	FLAG     byte = 0x01

	PROTOCOL_SIZE = 4
	FLAG_SIZE     = 1
	DATALEN_SIZE  = 4
	RESERVED_SIZE = 4
)

type ServerRequest struct {
	Request string              `json:"request"`
	Data    []ServerRequestData `json:"data"`
}

type ServerRequestData struct {
	Key     string `json:"key"`
	Timeout int    `json:"timeout"`
}

type AgentResponse struct {
	Version string              `json:"version"`
	Variant int                 `json:"variant"`
	Data    []AgentResponseData `json:"data"`
}

type AgentResponseData struct {
	Value interface{} `json:"value"`
}

func main() {
	l, _ := net.Listen("tcp", "0.0.0.0:10050")
	fmt.Println("Listening at :10050")
	for {
		conn, err := l.Accept()
		if err != nil {
			log.Fatal(err)
		}

		go newConnection(conn)
	}
}

func newConnection(conn net.Conn) {
	rAddr := conn.RemoteAddr()
	fmt.Println("New Connection ", rAddr)

	defer conn.Close()
	defer fmt.Printf("%s connection closed\n", rAddr)

	req, err := readRequest(conn)
	if err != nil {
		log.Println(err)
		return
	}
	fmt.Printf("[%s] Item key: %s\n", rAddr, req.Data[0].Key)

	if err := sendResponse(rand.Intn(20), conn); err != nil {
		log.Println(err)
		return
	}
}

func readRequest(r io.Reader) (*ServerRequest, error) {
	headerBuf := make([]byte, PROTOCOL_SIZE)
	if _, err := r.Read(headerBuf); err != nil {
		return nil, errors.New(fmt.Sprintf("Error while reading protocol, err: %s", err))
	}

	headerProtocol := string(headerBuf[:])
	if headerProtocol != PROTOCOL {
		return nil, errors.New(fmt.Sprintf("Unsupported protocol '%s'", headerProtocol))
	}

	headerBuf = make([]byte, FLAG_SIZE)
	if n, err := r.Read(headerBuf); err != nil {
		return nil, errors.New(fmt.Sprintf("Error while reading flag, err: %s", err))
	} else if n != FLAG_SIZE {
		return nil, errors.New(
			fmt.Sprintf(
				"Error while reading flag, err: invalid count of read bytes, expected %d read %d",
				FLAG_SIZE, n,
			),
		)
	}

	headerFlag := headerBuf[0]
	if headerFlag != FLAG {
		return nil, errors.New(fmt.Sprintf("Unsupported flag %x", headerFlag))
	}

	headerBuf = make([]byte, DATALEN_SIZE)
	if _, err := r.Read(headerBuf); err != nil {
		return nil, errors.New(fmt.Sprintf("Error while reading data length, err: %s", err))
	}
	dataLen := binary.LittleEndian.Uint32(headerBuf)

	headerBuf = make([]byte, RESERVED_SIZE)
	if _, err := r.Read(headerBuf); err != nil {
		return nil, errors.New(fmt.Sprintf("Error while reading reserved bytes, err: %s", err))
	}

	headerBuf = make([]byte, dataLen)
	if _, err := r.Read(headerBuf); err != nil {
		return nil, errors.New(fmt.Sprintf("Error while reading data, err %s", err))
	}

	request := &ServerRequest{}
	err := json.Unmarshal(headerBuf, request)

	return request, err
}

func sendResponse(value interface{}, w io.Writer) error {
	data := AgentResponse{
		Version: "7.0.0",
		Variant: 2,
		Data:    []AgentResponseData{{Value: value}},
	}
	jsonData, _ := json.Marshal(data)

	dataLen := make([]byte, 8)
	binary.LittleEndian.PutUint64(dataLen, uint64(len(jsonData)))

	res := []byte(PROTOCOL)
	res = append(res, FLAG)
	res = append(res, dataLen...)
	res = append(res, jsonData...)

	_, err := w.Write(res)

	return err
}
