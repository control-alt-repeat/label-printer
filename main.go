package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"

	localtunnel "github.com/localtunnel/go-localtunnel"
)

func main() {
	fmt.Print("Validating AWS credentials")

	_, err := session.NewSession(&aws.Config{
		Region: aws.String("eu-west-2")},
	)

	if err != nil {
		log.Fatalf("Failed to create AWS session: %v", err)
	}

	tunnel, err := localtunnel.Listen(localtunnel.Options{})

	if err != nil {
		log.Fatalf("Failed to create tunnel: %v", err)
	}

	fmt.Printf("Tunnel URL: %s\n", tunnel.URL())

	fmt.Println("Starting server")

	handlerMux := http.NewServeMux()

	server := &http.Server{
		Addr:         "127.0.0.1:8080",
		WriteTimeout: 10 * time.Second,
		ReadTimeout:  10 * time.Second,
		Handler:      middleware{handlerMux},
	}

	handlerMux.HandleFunc("/hello", hello)
	handlerMux.HandleFunc("/ping", ping)
	handlerMux.HandleFunc("/print-cat", printCat)

	ctx, cancel := context.WithCancel(context.Background())
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT)

	go func() {
		server.Serve(tunnel)
	}()

	defer func() {
		if err := server.Shutdown(ctx); err != nil {
			fmt.Println("error when shutting down the main server: ", err)
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

func printCat(rw http.ResponseWriter, req *http.Request) {
	switch req.Method {
	case http.MethodGet:
		cmd := exec.Command("brother_ql",
			"-b", "pyusb",
			"-m", "QL-500",
			"-p", "usb://0x04f9:0x2015",
			"print",
			"-l", "62",
			"cat-62x100.png")

		output, err := cmd.CombinedOutput()

		if err != nil {
			fmt.Printf("Command execution failed: %v", err)
		}

		rw.Write([]byte(string(output)))
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
	fmt.Println("request duration: ", time.Since(start))
}
