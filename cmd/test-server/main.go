package main

import (
	"fmt"
	"net/http"
)

func main() {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Println("Got it")
	})

	fmt.Println("Listening at :3000")
	http.ListenAndServe(":3000", nil)
}
