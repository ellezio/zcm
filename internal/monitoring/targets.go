package monitoring

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

	"gopkg.in/yaml.v3"
)

type targetsMetadata map[string]*targetInfo

type targetInfo struct {
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

type targetData struct {
	Start   time.Time
	Running bool

	LastResponseTime time.Duration
	LastStatus       string
	LastStatusCode   int
}

func LoadTargets(path string) (*Targets, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.New(fmt.Sprintf("Error while reading file, error: %s", err))
	}

	tm := targetsMetadata{}
	if err := yaml.Unmarshal(data, &tm); err != nil {
		return nil, err
	}

	if err := checkAndPrepareTargets(&tm); err != nil {
		return nil, err
	}

	t := &Targets{inner: tm}
	return t, nil
}

func checkAndPrepareTargets(targetsMetadata *targetsMetadata) error {
	for k, v := range *targetsMetadata {
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

type Targets struct {
	inner targetsMetadata
	data  sync.Map
}

func (t *Targets) StartMonitoring() {
	var wg sync.WaitGroup

	for name, target := range t.inner {
		t.data.Store(name, targetData{})
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

				if data, ok := t.GetData(key); ok {
					data.Start = time.Now()
					data.Running = true
					t.data.Store(key, data)
				}

				res, reqErr := client.Do(req)

				var deltaTime time.Duration

				if data, ok := t.GetData(key); ok {
					deltaTime = time.Since(data.Start)
					data.LastResponseTime = deltaTime
					data.Running = false

					if res != nil {
						data.LastStatus = res.Status
						data.LastStatusCode = res.StatusCode
					} else if reqErr != nil {
						data.LastStatus = ""
						data.LastStatusCode = 0
					}

					t.data.Store(key, data)
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

func (t *Targets) GetData(key string) (targetData, bool) {
	if s, ok := t.data.Load(key); ok {
		if data, ok := s.(targetData); ok {
			return data, true
		}
	}

	return targetData{}, false
}
