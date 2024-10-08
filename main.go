package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	localtunnel "github.com/localtunnel/go-localtunnel"
)

func main() {
	tunnel, err := localtunnel.Listen(localtunnel.Options{})

	if err != nil {
		log.Fatalf("Failed to create tunnel: %v", err)
	}

	fmt.Printf("Tunnel URL: %s\n", tunnel.URL())

	fmt.Println("Starting server")

	helloMux := http.NewServeMux()
	helloMux.HandleFunc("/hello", hello)

	helloSrv := &http.Server{
		Addr:         "127.0.0.1:8080",
		WriteTimeout: 10 * time.Second,
		ReadTimeout:  10 * time.Second,
		Handler:      middleware{helloMux},
	}

	adminMux := http.NewServeMux()
	adminMux.HandleFunc("/ping", ping)

	adminSrv := &http.Server{
		Addr:         "127.0.0.1:8081",
		WriteTimeout: 10 * time.Second,
		ReadTimeout:  10 * time.Second,
		Handler:      middleware{adminMux},
	}

	ctx, cancel := context.WithCancel(context.Background())
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT)

	go func() {
		helloSrv.Serve(tunnel)
	}()

	go func() {
		adminSrv.Serve(tunnel)
	}()

	defer func() {
		if err := helloSrv.Shutdown(ctx); err != nil {
			fmt.Println("error when shutting down the main server: ", err)
		}
		if err := adminSrv.Shutdown(ctx); err != nil {
			fmt.Println("error when shutting down the admin server: ", err)
		}
	}()

	sig := <-sigs
	fmt.Println(sig)

	cancel()

	fmt.Println("service has shutdown")
}

func hello(rw http.ResponseWriter, req *http.Request) {
	u := req.Context().Value("user")
	user := "unset"
	if u != nil {
		user = u.(string)
	}

	switch req.Method {
	case http.MethodGet:
		if _, err := rw.Write([]byte("Hello " + user + "\n")); err != nil {
			fmt.Println("error when writing response for /hello request")
			rw.WriteHeader(http.StatusInternalServerError)
		}
	case http.MethodPost:
		if _, err := rw.Write([]byte("Thanks for posting to me, " + user + "\n")); err != nil {
			fmt.Println("error when writing response for /hello request")
			rw.WriteHeader(http.StatusInternalServerError)
		}
	}
}

func ping(rw http.ResponseWriter, req *http.Request) {
	switch req.Method {
	case http.MethodGet:
		if _, err := rw.Write([]byte("pong\n")); err != nil {
			fmt.Println("error when writing response for /ping request")
			rw.WriteHeader(http.StatusInternalServerError)
		}
	}
}

type middleware struct {
	mux http.Handler
}

func (m middleware) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	ctx := context.WithValue(req.Context(), "user", "unknown")
	ctx = context.WithValue(ctx, "__requestStartTimer__", time.Now())
	req = req.WithContext(ctx)

	m.mux.ServeHTTP(rw, req)

	start := req.Context().Value("__requestStartTimer__").(time.Time)
	fmt.Println("request duration: ", time.Now().Sub(start))
}
