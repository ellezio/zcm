package main

import (
	"bytes"
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
	Method   string            `yaml:"method"`
	FormData map[string]string `yaml:"form-data"`
}

type MonitoringState = sync.Map
type MonitoringStateData struct {
	Start             *time.Time
	LastExecutionTime time.Duration
	Running           bool
}

func main() {
	var mstate MonitoringState

	mts, err := loadMonitoringTargets()
	if err != nil {
		log.Fatalf("Error while loading monitoring targets, err: %s", err)
	}

	go startMonitoring(*mts, &mstate)

	l, _ := net.Listen("tcp", "0.0.0.0:10050")
	fmt.Println("Listening at :10050")
	for {
		conn, err := l.Accept()
		if err != nil {
			log.Fatal(err)
		}

		go newConnection(conn, itemHandler(&mstate))
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

func startMonitoring(targets MonitoringTargets, state *MonitoringState) {
	var wg sync.WaitGroup

	for name, target := range targets {
		state.Store(name, MonitoringStateData{})
		wg.Add(1)

		go func(key string) {
			defer wg.Done()

			for {
				var (
					res *http.Response
					err error
					// deltaTime time.Duration
				)

				if target.Method == http.MethodPost {
					var (
						body        io.Reader
						contentType string
					)

					if target.FormData != nil {
						values := url.Values{}
						for k, v := range target.FormData {
							values.Add(k, v)
						}
						body = bytes.NewBuffer([]byte(values.Encode()))
						contentType = "application/x-www-form-urlencoded"
					} else {
						contentType = "application/json"
					}

					req, _ := http.NewRequest(
						target.Method,
						target.Url,
						body,
					)
					req.Header.Set("Content-Type", contentType+"; charset=utf-8")

					client := http.Client{
						Timeout: time.Second * 10,
					}

					start := time.Now()

					if s, ok := state.Load(key); ok {
						if msd, ok := s.(MonitoringStateData); ok {
							msd.Start = &start
							msd.Running = true
							state.Store(key, msd)
						}
					}

					res, err = client.Do(req)
					// deltaTime = time.Since(start)
				} else if target.Method == http.MethodGet {
					start := time.Now()
					if s, ok := state.Load(key); ok {
						if msd, ok := s.(MonitoringStateData); ok {
							msd.Start = &start
							msd.Running = true
							state.Store(key, msd)
						}
					}
					res, err = http.Get(target.Url)
				}

				if s, ok := state.Load(key); ok {
					if msd, ok := s.(MonitoringStateData); ok {
						msd.LastExecutionTime = time.Since(*msd.Start)
						msd.Start = nil
						msd.Running = false

						state.Store(key, msd)
					}
				}

				if err != nil {
					log.Println(err)
				} else if res != nil && res.StatusCode != http.StatusOK {
					log.Printf("[%s] not 2XX response code, code: %d", name, res.StatusCode)
				} else if res != nil {
					// fmt.Printf("%d ms\n", deltaTime.Milliseconds())
					res.Body.Close()
				} else {
					log.Printf("[%s] invalid method \"%s\"", name, target.Method)
				}

				time.Sleep(time.Second * time.Duration(target.Interval))
			}
		}(name)
	}

	wg.Wait()
}

func itemHandler(state *MonitoringState) func(string) interface{} {
	return func(key string) interface{} {
		if s, ok := state.Load(key); ok {
			if msd, ok := s.(MonitoringStateData); ok {
				v := msd.LastExecutionTime.Milliseconds()
				if msd.Running && v < time.Since(*msd.Start).Milliseconds() {
					v = time.Since(*msd.Start).Milliseconds()
				}
				fmt.Printf("read key \"%s\" and get value %dms\n", key, v)
				return v
			}
		}

		return nil
	}
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
