package main

import (
	"fmt"
	"net/http"
	"time"
)

func main() {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Println("start")
		fmt.Println("wait 300ms")
		time.Sleep(300 * time.Millisecond)
		fmt.Println("end")
	})

	fmt.Println("Listening at :3000")
	http.ListenAndServe(":3000", nil)
}
