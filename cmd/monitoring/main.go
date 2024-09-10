package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/pawellendzion/zcm/internal/zbx"

	"gopkg.in/yaml.v3"
)

type monitoringTargets map[string]*monitoringTarget

type monitoringTarget struct {
	Url          string            `yaml:"url"`
	Autorization autorization      `yaml:"autorization"`
	Interval     int               `yaml:"interval"`
	Method       string            `yaml:"method"`
	FormData     map[string]string `yaml:"form-data"`
	Json         string            `yaml:"json"`
}

type autorization struct {
	Type     string
	Username string
	Password string
	Token    string
}

type monitoringState = sync.Map
type monitoringStateData struct {
	Start             time.Time
	LastExecutionTime time.Duration
	Running           bool
}

func main() {
	var mstate monitoringState

	mts, err := loadMonitoringTargets()
	if err != nil {
		log.Fatalf("Error while loading monitoring targets, err: %s", err)
	}

	if err := checkAndPrepareTargets(mts); err != nil {
		log.Fatal("Error while parsing target ", err)
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
				Timeout: time.Minute * 10,
			}

			for {
				var (
					body        io.Reader
					contentType string
				)

				if target.Method == http.MethodPost {
					if target.FormData != nil {
						contentType = "application/x-www-form-urlencoded"

						values := url.Values{}
						for k, v := range target.FormData {
							values.Add(k, v)
						}
						body = bytes.NewBuffer([]byte(values.Encode()))
					} else if target.Json != "" {
						contentType = "application/json"
						buf := &bytes.Buffer{}
						_ = json.Compact(buf, []byte(target.Json))
						body = buf
					}

				}

				req, _ := http.NewRequest(
					target.Method,
					target.Url,
					body,
				)

				if contentType != "" {
					req.Header.Set("Content-Type", contentType+"; charset=utf-8")
				}

				if target.Autorization.Type != "" {
					token := target.Autorization.Token
					if token == "" {
						auth := target.Autorization.Username + ":" + target.Autorization.Password
						token = base64.StdEncoding.EncodeToString([]byte(auth))
					}
					req.Header.Set("Authorization", target.Autorization.Type+" "+token)
				}

				if s, ok := state.Load(key); ok {
					if msd, ok := s.(monitoringStateData); ok {
						msd.Start = time.Now()
						msd.Running = true
						state.Store(key, msd)
					}
				}

				res, reqErr := client.Do(req)

				var deltaTime time.Duration

				if s, ok := state.Load(key); ok {
					if msd, ok := s.(monitoringStateData); ok {
						deltaTime = time.Since(msd.Start)
						msd.LastExecutionTime = deltaTime
						msd.Running = false
						state.Store(key, msd)
					}
				}

				if reqErr != nil {
					log.Println("request error: ", reqErr)
				} else if res != nil && res.StatusCode >= 200 && res.StatusCode < 300 {
					fmt.Printf("%d ms\n", deltaTime.Milliseconds())
				} else {
					b, _ := io.ReadAll(res.Body)
					log.Printf("[%s] not 2XX response code, code: %d, body: %s", name, res.StatusCode, b)
				}

				if reqErr == nil {
					_, _ = io.ReadAll(res.Body)
					res.Body.Close()
				}

				time.Sleep(time.Millisecond * time.Duration(target.Interval))
			}
		}(name)
	}

	wg.Wait()
}

func checkAndPrepareTargets(targets *monitoringTargets) error {
	for k, v := range *targets {
		if v.Interval == 0 {
			v.Interval = 10000
		}

		if v.Url == "" {
			return errors.New(fmt.Sprintf("%s: field url not specifaied", k))
		}

		if v.Method == "" {
			v.Method = http.MethodGet
		} else {
			v.Method = strings.ToUpper(v.Method)
			if !isHTTPMethodSupported(v.Method) {
				return errors.New(fmt.Sprintf("%s: http method %s not supported", k, v.Method))
			}
		}

		if v.Method == http.MethodPost {
			if v.Json == "" && v.FormData == nil {
				return errors.New(fmt.Sprintf("%s: when http method is POST field \"json\" or \"form-data\" is required", k))
			}

			if v.Json != "" && v.FormData != nil {
				return errors.New(fmt.Sprintf("%s: field \"json\" and \"form-data\" cannot be filled together", k))
			}
		}

		if v.Autorization != (autorization{}) {
			if v.Autorization.Type == "" {
				return errors.New(fmt.Sprintf("%s: field \"type\" is required for autorization", k))
			}

			if v.Autorization.Token != "" && (v.Autorization.Username != "" || v.Autorization.Password != "") {
				return errors.New(fmt.Sprintf("%s: \"token\" cannot be filled along with \"username\" and \"password\"", k))
			}

			if v.Autorization.Token == "" && (v.Autorization.Username == "" || v.Autorization.Password == "") {
				return errors.New(fmt.Sprintf("%s: token or username and password is required for autorization", k))
			}
		}
	}

	return nil
}

func isHTTPMethodSupported(method string) bool {
	return method == http.MethodGet || method == http.MethodPost
}

func itemHandler(state *monitoringState) func(string) interface{} {
	return func(key string) interface{} {
		if s, ok := state.Load(key); ok {
			if msd, ok := s.(monitoringStateData); ok {
				v := msd.LastExecutionTime.Milliseconds()
				if msd.Running && v < time.Since(msd.Start).Milliseconds() {
					v = time.Since(msd.Start).Milliseconds()
				}
				fmt.Printf("read key \"%s\" and get value %dms\n", key, v)
				return v
			}
		}

		return nil
	}
}
