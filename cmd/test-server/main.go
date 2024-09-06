package main

import (
	"fmt"
	"os"
)

func main() {
	b, err := os.ReadFile("monitoring-targets.yml")
	fmt.Println(err)
	fmt.Println(string(b))
	// http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
	// 	fmt.Println("Got it")
	// })
	//
	// fmt.Println("Listening at :3000")
	// http.ListenAndServe(":3000", nil)
}
