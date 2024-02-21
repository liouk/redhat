package main

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
)

func main() {
	certFile := os.Args[1]
	keyFile := os.Args[2]

	listenerAddress := "127.0.0.1:45011"
	ln, err := net.Listen("tcp", listenerAddress)
	if err != nil {
		panic(err)
	}
	defer func() {
		ln.Close()
	}()
	serverAddress := ln.Addr().String()
	serverPort := serverAddress[strings.LastIndex(serverAddress, ":")+1:]
	srv := http.Server{}

	c := make(chan os.Signal)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		fmt.Println("shutting down...")
		err := srv.Close()
		if err != nil {
			panic(err)
		}
	}()

	// Start a server configured with the cert and key
	fmt.Println("listening on port", serverPort)
	if err := srv.ServeTLS(ln, certFile, keyFile); err != nil && err != http.ErrServerClosed {
		panic(err)
	}
}
