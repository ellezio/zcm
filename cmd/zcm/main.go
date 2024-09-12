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
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/ellezio/zcm/internal/zbx"

	"gopkg.in/yaml.v3"
)

type monitoringTargets map[string]*monitoringTarget

type monitoringTarget struct {
	Url           string            `yaml:"url"`
	Authorization authorization     `yaml:"authorization"`
	Interval      int               `yaml:"interval"`
	Method        string            `yaml:"method"`
	FormData      map[string]string `yaml:"form-data"`
	Json          string            `yaml:"json"`
}

type authorization struct {
	Type     string `yaml:"type"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	Token    string `yaml:"token"`
}

type monitoringState = sync.Map
type monitoringStateData struct {
	Start   time.Time
	Running bool

	LastResponseTime time.Duration
	LastStatus       string
	LastStatusCode   int
}

func main() {
	var mstate monitoringState

	mts, err := loadMonitoringTargets("monitoring-targets.yml")
	if err != nil {
		log.Fatalf("Error while loading monitoring targets, error: %s", err)
	}

	if err := checkAndPrepareTargets(mts); err != nil {
		log.Fatal(err)
	}

	go startMonitoring(*mts, &mstate)

	port := os.Getenv("ZCM_PORT")
	if port == "" {
		port = "10050"
	}

	address := fmt.Sprintf("0.0.0.0:%s", port)
	log.Println("Listening at", address)
	if err := zbx.ListenAndServe(address, itemHandler(&mstate)); err != nil {
		log.Fatal(err)
	}
}

func loadMonitoringTargets(path string) (*monitoringTargets, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.New(fmt.Sprintf("Error while reading file, error: %s", err))
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
						body = bytes.NewBufferString(target.Json)
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

				if target.Authorization.Type != "" {
					token := target.Authorization.Token
					if token == "" {
						auth := target.Authorization.Username + ":" + target.Authorization.Password
						token = base64.StdEncoding.EncodeToString([]byte(auth))
					}
					req.Header.Set("Authorization", target.Authorization.Type+" "+token)
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
						msd.LastResponseTime = deltaTime
						msd.Running = false

						if res != nil {
							msd.LastStatus = res.Status
							msd.LastStatusCode = res.StatusCode
						} else if reqErr != nil {
							msd.LastStatus = ""
							msd.LastStatusCode = 0
						}

						state.Store(key, msd)
					}
				}

				if reqErr != nil {
					log.Println("request error: ", reqErr)
				} else {
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

			if v.Json != "" {
				buf := &bytes.Buffer{}
				if err := json.Compact(buf, []byte(v.Json)); err != nil {
					return errors.New(fmt.Sprintf("%s: error while parsing json data, error: %s", k, err))
				}
				v.Json = buf.String()
			}
		}

		if v.Authorization != (authorization{}) {
			if v.Authorization.Type == "" {
				return errors.New(fmt.Sprintf("%s: field \"type\" is required for authorization", k))
			}

			if v.Authorization.Token != "" && (v.Authorization.Username != "" || v.Authorization.Password != "") {
				return errors.New(fmt.Sprintf("%s: \"token\" cannot be filled along with \"username\" and \"password\"", k))
			}

			if v.Authorization.Token == "" && (v.Authorization.Username == "" || v.Authorization.Password == "") {
				return errors.New(fmt.Sprintf("%s: token or username and password is required for authorization", k))
			}
		}

		if err := replaceWithEnvVar(&v.Url); err != nil {
			return err
		}

		if err := replaceWithEnvVar(&v.Authorization.Token); err != nil {
			return err
		}

		if err := replaceWithEnvVar(&v.Authorization.Password); err != nil {
			return err
		}

		if err := replaceWithEnvVar(&v.Authorization.Username); err != nil {
			return err
		}

		if err := replaceWithEnvVar(&v.Authorization.Type); err != nil {
			return err
		}
	}

	return nil
}

func isHTTPMethodSupported(method string) bool {
	return method == http.MethodGet || method == http.MethodPost
}

func replaceWithEnvVar(value *string) error {
	reg := regexp.MustCompile("{env:([a-zA-Z_]{1}[a-zA-Z_0-9]*)}")
	matches := reg.FindAllStringSubmatch(*value, -1)
	for _, matched := range matches {
		envVal := os.Getenv(matched[1])
		if envVal == "" {
			return errors.New(fmt.Sprintf("environment variable %s is not present", matched[1]))
		}
		*value = strings.ReplaceAll(*value, matched[0], envVal)
	}

	return nil
}

func itemHandler(state *monitoringState) func(string) interface{} {
	return func(key string) interface{} {
		sep := strings.LastIndex(key, ".")
		if sep == -1 {
			log.Printf("item key \"%s\" doesn't specify parameter (<item>.<parameter>)", key)
			return nil
		}

		item := key[:sep]
		param := key[sep+1:]

		if s, ok := state.Load(item); ok {
			if msd, ok := s.(monitoringStateData); ok {
				var value interface{}

				switch param {
				case "responseTime":
					v := msd.LastResponseTime.Milliseconds()
					if msd.Running && v < time.Since(msd.Start).Milliseconds() {
						v = time.Since(msd.Start).Milliseconds()
					}
					value = v

				case "statusCode":
					value = msd.LastStatusCode

				case "status":
					value = msd.LastStatus

				default:
					log.Printf("item key: %s, unknown parameter: %s", key, param)
					return nil
				}

				log.Printf("item key: %s, value: %v", key, value)
				return value
			}

		}

		log.Printf("unsupported item key: %s", key)
		return nil
	}
}
