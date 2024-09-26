package main

import (
	"fmt"
	"net/http"
	"time"
)

func livez(w http.ResponseWriter, r *http.Request) {
	fmt.Printf("[%s] livez probe received\n", time.Now())
	time.Sleep(5 * time.Second)
	fmt.Printf("[%s] livez probe will return\n", time.Now())

	w.WriteHeader(http.StatusOK)
	fmt.Fprintln(w, "ok")
}

func main() {
	http.HandleFunc("/livez", livez)

	fmt.Println("Server is listening on port 8080...")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		fmt.Println("Error starting server:", err)
	}
}
