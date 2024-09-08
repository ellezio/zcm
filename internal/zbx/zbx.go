package zbx

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"time"
)

const (
	protocol      = "ZBXD"
	flag     byte = 0x01

	protocolSize = 4
	flagSize     = 1
	datalenSize  = 4
	reservedSize = 4
)

type serverRequest struct {
	Request string              `json:"request"`
	Data    []serverRequestData `json:"data"`
}

type serverRequestData struct {
	Key     string `json:"key"`
	Timeout int    `json:"timeout"`
}

type agentResponse struct {
	Version string              `json:"version"`
	Variant int                 `json:"variant"`
	Data    []agentResponseData `json:"data"`
}

type agentResponseData struct {
	Value interface{} `json:"value"`
}

func ListenAndServe(address string, handler func(itemKey string) interface{}) error {
	l, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}

	defer l.Close()

	var tempDelay time.Duration // how long to sleep on accept failure

	for {
		conn, err := l.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return err
			}

			if tempDelay == 0 {
				tempDelay = 5 * time.Millisecond
			} else {
				tempDelay *= 2
			}
			if max := 1 * time.Second; tempDelay > max {
				tempDelay = max
			}
			log.Printf("Accept error: %s; retrying in %v", err, tempDelay)
			time.Sleep(tempDelay)
			continue
		}

		go handleConn(conn, handler)
	}
}

func handleConn(conn net.Conn, handler func(key string) interface{}) {
	defer conn.Close()

	req, err := decode(conn)
	if err != nil {
		log.Println(err)
		return
	}

	value := handler(req.Data[0].Key)

	encodedValue, err := encode(value)
	if err != nil {
		log.Println(err)
		return
	}

	if _, err := conn.Write(encodedValue); err != nil {
		log.Println(err)
	}
}

func readHeader(r io.Reader, what string, size uint32) ([]byte, error) {
	buf := make([]byte, protocolSize)

	if n, err := r.Read(buf); err != nil && err != io.EOF {
		return nil, errors.New(fmt.Sprintf("Error while reading %s; error: %s", what, err))
	} else if uint32(n) < size {
		return nil, errors.New(
			fmt.Sprintf(
				"Error while reading %s, error: %s",
				what, io.ErrUnexpectedEOF.Error(),
			),
		)
	}

	return buf, nil
}

func decode(r io.Reader) (*serverRequest, error) {
	b, err := readHeader(r, "protocol", protocolSize)
	if err != nil {
		return nil, err
	}

	headerProtocol := string(b[:])
	if headerProtocol != protocol {
		return nil, errors.New(fmt.Sprintf("Unsupported protocol '%s'", headerProtocol))
	}

	b, err = readHeader(r, "flag", flagSize)
	if err != nil {
		return nil, err
	}

	headerFlag := b[0]
	if headerFlag != flag {
		return nil, errors.New(fmt.Sprintf("Unsupported flag %x", headerFlag))
	}

	b, err = readHeader(r, "data length", datalenSize)
	if err != nil {
		return nil, err
	}

	dataLen := binary.LittleEndian.Uint32(b)

	b, err = readHeader(r, "reserved bytes", reservedSize)
	if err != nil {
		return nil, err
	}

	b, err = readHeader(r, "data", dataLen)
	if err != nil {
		return nil, err
	}

	req := &serverRequest{}
	err = json.Unmarshal(b, req)

	return req, err
}

func encode(value interface{}) ([]byte, error) {
	data := agentResponse{
		Version: "7.0.0",
		Variant: 2,
		Data:    []agentResponseData{{Value: value}},
	}

	jsonData, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}

	dataLen := make([]byte, 8)
	binary.LittleEndian.PutUint64(dataLen, uint64(len(jsonData)))

	res := []byte(protocol)
	res = append(res, flag)
	res = append(res, dataLen...)
	res = append(res, jsonData...)

	return res, nil
}
