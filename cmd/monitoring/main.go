package main

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/pawellendzion/zcm/internal/zbx"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

type monitoringTargets map[string]monitoringTarget

type monitoringTarget struct {
	Url      string            `yaml:"url"`
	Interval float32           `yaml:"interval"`
	Method   string            `yaml:"method"`
	FormData map[string]string `yaml:"form-data"`
}

type monitoringState = sync.Map
type monitoringStateData struct {
	Start             *time.Time
	LastExecutionTime time.Duration
	Running           bool
}

func main() {
	var mstate monitoringState

	mts, err := loadMonitoringTargets()
	if err != nil {
		log.Fatalf("Error while loading monitoring targets, err: %s", err)
	}

	go startMonitoring(*mts, &mstate)

	log.Println("Listening at :10050")
	if err := zbx.ListenAndServe("0.0.0.0:10050", itemHandler(&mstate)); err != nil {
		log.Fatal(err)
	}
}

func loadMonitoringTargets() (*monitoringTargets, error) {
	data, err := os.ReadFile("monitoring-targets.yml")
	if err != nil {
		return nil, errors.New(fmt.Sprintf("Error while reading file, err: %s", err))
	}

	mts := monitoringTargets{}
	if err := yaml.Unmarshal(data, &mts); err != nil {
		return nil, err
	}

	return &mts, nil
}

func startMonitoring(targets monitoringTargets, state *monitoringState) {
	var wg sync.WaitGroup

	for name, target := range targets {
		state.Store(name, monitoringStateData{})
		wg.Add(1)

		go func(key string) {
			defer wg.Done()

			client := http.Client{
				Timeout: time.Second * 10,
			}

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

					start := time.Now()

					if s, ok := state.Load(key); ok {
						if msd, ok := s.(monitoringStateData); ok {
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
						if msd, ok := s.(monitoringStateData); ok {
							msd.Start = &start
							msd.Running = true
							state.Store(key, msd)
						}
					}
					res, err = http.Get(target.Url)
				}

				if s, ok := state.Load(key); ok {
					if msd, ok := s.(monitoringStateData); ok {
						msd.LastExecutionTime = time.Since(*msd.Start)
						msd.Start = nil
						msd.Running = false

						state.Store(key, msd)
					}
				}

				if err != nil {
					log.Println("request error: ", err)
				} else if res != nil && res.StatusCode != http.StatusOK {
					log.Printf("[%s] not 2XX response code, code: %d", name, res.StatusCode)
				} else if res != nil {
					// fmt.Printf("%d ms\n", deltaTime.Milliseconds())
				} else {
					log.Printf("[%s] invalid method \"%s\"", name, target.Method)
				}

				if err == nil {
					res.Body.Close()
				}

				time.Sleep(time.Second * time.Duration(target.Interval))
			}
		}(name)
	}

	wg.Wait()
}

func itemHandler(state *monitoringState) func(string) interface{} {
	return func(key string) interface{} {
		if s, ok := state.Load(key); ok {
			if msd, ok := s.(monitoringStateData); ok {
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
