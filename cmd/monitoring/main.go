package main

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
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

type MonitoringTargets map[string]MonitoringTarget

type MonitoringTarget struct {
	Url      string            `yaml:"url"`
	Interval float32           `yaml:"interval"`
	FormData map[string]string `yaml:"form-data"`
}

func main() {
	mts, err := loadMonitoringTargets()
	if err != nil {
		log.Fatalf("Error while loading monitoring targets, err: %s", err)
	}

	go startMonitoring(*mts)

	l, _ := net.Listen("tcp", "0.0.0.0:10050")
	fmt.Println("Listening at :10050")
	for {
		conn, err := l.Accept()
		if err != nil {
			log.Fatal(err)
		}

		go newConnection(conn, itemHandler)
	}
}

func loadMonitoringTargets() (*MonitoringTargets, error) {
	data, err := os.ReadFile("monitoring-targets.yml")
	if err != nil {
		return nil, errors.New(fmt.Sprintf("Error while reading file, err: %s", err))
	}

	mts := MonitoringTargets{}
	if err := yaml.Unmarshal(data, &mts); err != nil {
		return nil, err
	}

	return &mts, nil
}

func startMonitoring(targets MonitoringTargets) {
	var wg sync.WaitGroup

	for name, target := range targets {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for {
				values := url.Values{}
				for k, v := range target.FormData {
					values.Add(k, v)
				}

				start := time.Now()
				res, err := http.PostForm(target.Url, values)

				if err != nil {
					log.Println(err)
				} else if res.StatusCode != http.StatusOK {
					log.Printf("[%s] not 2XX response code, code: %d", name, res.StatusCode)
				} else {
					delta := time.Since(start).Milliseconds()
					fmt.Printf("%d ms\n", delta)
				}

				time.Sleep(time.Second * time.Duration(target.Interval))
			}
		}()
	}

	wg.Wait()
}

func itemHandler(key string) interface{} {
	return 0
}

func newConnection(conn net.Conn, handler func(key string) interface{}) {
	rAddr := conn.RemoteAddr()
	fmt.Println("\nNew Connection", rAddr)

	defer conn.Close()
	defer fmt.Printf("%s connection closed\n", rAddr)

	req, err := readRequest(conn)
	if err != nil {
		log.Println(err)
		return
	}
	fmt.Printf("[%s] Item key: %s\n", rAddr, req.Data[0].Key)

	value := handler(req.Data[0].Key)

	if err := sendResponse(value, conn); err != nil {
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
