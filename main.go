package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/ssm"

	localtunnel "github.com/localtunnel/go-localtunnel"
)

type Printer struct {
	Port string
	Name string
}

var printerMap = map[string]Printer{
	"62": {
		Port: "usb://0x04f9:0x2015",
		Name: "QL-500",
	},
	"102x152": {
		Port: "usb://0x04f9:0x202a",
		Name: "QL-1060N",
	},
}

func main() {
	aws_session, err := session.NewSession(&aws.Config{
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

	input := &ssm.PutParameterInput{
		Name:      aws.String("/control_alt_repeat/ebay/live/label_printer/host_domain"),
		Value:     aws.String(tunnel.URL()),
		Type:      aws.String("String"),
		Overwrite: aws.Bool(true),
	}

	ssm_svc := ssm.New(aws_session)
	_, err = ssm_svc.PutParameter(input)

	if err != nil {
		log.Fatalf("Failed to store tunnel URL in parameter store: %v", err)
	}

	fmt.Println("Starting server")

	handlerMux := http.NewServeMux()

	server := &http.Server{
		Addr:         "127.0.0.1:8080",
		WriteTimeout: 30 * time.Second,
		ReadTimeout:  30 * time.Second,
		Handler:      middleware{handlerMux},
	}

	handlerMux.HandleFunc("/webhook", webhook)
	handlerMux.HandleFunc("/ping", ping)
	handlerMux.HandleFunc("/print-cat", printCat)
	handlerMux.HandleFunc("/print", print)

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

func print(rw http.ResponseWriter, req *http.Request) {
	fmt.Println("/print")
	switch req.Method {
	case http.MethodPost:
		fmt.Println("Checking size < 10MB")
		req.ParseMultipartForm(10 << 20)

		fmt.Println("Retrieving image from form")
		file, header, err := req.FormFile("image")
		if err != nil {
			http.Error(rw, "Error retrieving the file", http.StatusBadRequest)
			return
		}
		defer file.Close()

		filePath := filepath.Join("uploads", header.Filename)
		fmt.Println("Creating file: ", filePath)
		out, err := os.Create(filePath)
		if err != nil {
			http.Error(rw, "Error creating the file", http.StatusInternalServerError)
			return
		}
		defer out.Close()

		_, err = io.Copy(out, file)
		if err != nil {
			http.Error(rw, "Error saving the file", http.StatusInternalServerError)
			return
		}

		fmt.Println("Getting the format")
		format_name := strings.SplitN(header.Filename, "-", 2)[0]

		fmt.Println("format_name: ", format_name)

		printer := printerMap[format_name]

		fmt.Println("printer: ", printer.Name)
		fmt.Println("port: ", printer.Port)

		cmd := exec.Command("brother_ql",
			"--backend", "pyusb",
			"--model", printer.Name,
			"--printer", printer.Port,
			"print",
			"-l", format_name,
			header.Filename)

		fmt.Println("Executing command: ", cmd.String())

		_, err = cmd.CombinedOutput()
		if err != nil {
			http.Error(rw, "Error executing print operation the file", http.StatusInternalServerError)
			return
		}

		err = os.Remove(filePath)
		if err != nil {
			http.Error(rw, "Error deleting the file", http.StatusInternalServerError)
			return
		}

		fmt.Fprintln(rw, "File uploaded and processed successfully")
	}
}

func webhook(rw http.ResponseWriter, req *http.Request) {
	switch req.Method {
	case http.MethodPost:
		aws_session, err := session.NewSession(&aws.Config{
			Region: aws.String("eu-west-2")},
		)
		if err != nil {
			fmt.Println("Failed to create new session", err)
			return
		}

		svc := s3.New(aws_session)
		bucket := "control-alt-repeat-label-print-buffer"

		result, err := svc.ListObjectsV2(&s3.ListObjectsV2Input{
			Bucket: aws.String(bucket),
		})
		if err != nil {
			log.Fatal(err)
			return
		}

		for _, item := range result.Contents {
			err := downloadFile(svc, bucket, *item.Key)
			if err != nil {
				log.Fatal(err)
				return
			}

			format_name := strings.SplitN(*item.Key, "-", 2)[0]

			fmt.Println("format_name: ", format_name)

			printer := printerMap[format_name]

			fmt.Println("printer: ", printer.Name)
			fmt.Println("port: ", printer.Port)

			cmd := exec.Command("brother_ql",
				"--backend", "pyusb",
				"--model", printer.Name,
				"--printer", printer.Port,
				"print",
				"-l", format_name,
				*item.Key)

			fmt.Println("Executing command: ", cmd.String())

			output, err := cmd.CombinedOutput()

			if err != nil {
				fmt.Println(string(output))
				fmt.Println(string(err.Error()))
			} else {
				deleteFile(svc, bucket, *item.Key)
			}

			fmt.Println(string(output))
		}

		rw.Write([]byte(""))
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
			log.Fatal(err)
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

func downloadFile(svc *s3.S3, bucket, key string) error {
	fmt.Printf("Downloading %s from bucket %s\n", key, bucket)

	file, err := os.Create(filepath.Base(key))
	if err != nil {
		return err
	}
	defer file.Close()

	result, err := svc.GetObject(&s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})

	if err != nil {
		return err
	}

	_, err = io.Copy(file, result.Body)

	return err
}

func deleteFile(svc *s3.S3, bucket, key string) error {
	_, err := svc.DeleteObject(&s3.DeleteObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})

	return err
}
