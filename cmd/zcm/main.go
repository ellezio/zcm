package main

import (
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/ellezio/zcm/internal/monitoring"
	"github.com/ellezio/zcm/internal/zbx"
)

func main() {
	targets, err := monitoring.LoadTargets("monitoring-targets.yml")
	if err != nil {
		log.Fatal(err)
	}

	go targets.StartMonitoring()

	port := os.Getenv("ZCM_PORT")
	if port == "" {
		port = "10050"
	}

	address := fmt.Sprintf("0.0.0.0:%s", port)
	log.Println("Listening at", address)
	if err := zbx.ListenAndServe(address, itemHandler(targets)); err != nil {
		log.Fatal(err)
	}
}

func itemHandler(targets *monitoring.Targets) func(string) interface{} {
	return func(key string) interface{} {
		sep := strings.LastIndex(key, ".")
		if sep == -1 {
			log.Printf("item key \"%s\" doesn't specify parameter (<item>.<parameter>)", key)
			return nil
		}

		itemKey := key[:sep]
		param := key[sep+1:]

		if data, ok := targets.GetData(itemKey); ok {
			var value interface{}

			switch param {
			case "responseTime":
				v := data.LastResponseTime.Milliseconds()
				if data.Running && v < time.Since(data.Start).Milliseconds() {
					v = time.Since(data.Start).Milliseconds()
				}
				value = v

			case "statusCode":
				value = data.LastStatusCode

			case "status":
				value = data.LastStatus

			default:
				log.Printf("item key: %s, unknown parameter: %s", key, param)
				return nil
			}

			log.Printf("item key: %s, value: %v", key, value)
			return value
		}

		log.Printf("unsupported item key: %s", key)
		return nil
	}
}
