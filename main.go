package main

import (
	"context"
	"encoding/json"
	"fmt"
	"image/png"
	"io"
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
	"github.com/aws/aws-sdk-go/service/ssm"

	localtunnel "github.com/localtunnel/go-localtunnel"

	"github.com/justinas/alice"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/hlog"
	"github.com/rs/zerolog/pkgerrors"
)

var log zerolog.Logger

type PrintJob struct {
	Printer    Printer
	FormatName string
	FilePath   string
}

type Printer struct {
	Port string
	Name string
}

type LabelDimensions struct {
	X int
	Y int
}

type LabelFormat struct {
	Name string
}

var labelFormats = map[LabelDimensions]LabelFormat{
	{X: 696, Y: 1109}: {
		Name: "62x100",
	},
	{X: 1164, Y: 1660}: {
		Name: "102x152",
	},
}

// https://github.com/pklaus/brother_ql/blob/56cf4394ad750346c6b664821ccd7489ec140dae/brother_ql/labels.py#L91
var labelPrinters = map[LabelFormat]Printer{
	{Name: "62x100"}: {
		Name: "QL-500",
		Port: "usb://0x04f9:0x2015",
	},
	{Name: "102x152"}: {
		Name: "QL-1060N",
		Port: "usb://0x04f9:0x202a",
	},
}

const (
	ServiceName            = "label-printer"
	UploadDirectory        = "uploads"
	Region                 = "eu-west-2"
	TunnelURLParameterName = "/control_alt_repeat/ebay/live/label_printer/host_domain"
)

func main() {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	zerolog.ErrorStackMarshaler = pkgerrors.MarshalStack

	consoleWriter := zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: time.RFC3339}

	log = zerolog.New(consoleWriter).
		With().
		Timestamp().
		Str("service", ServiceName).
		Logger().
		Level(zerolog.DebugLevel)

	if err := createUploadDirectory(); err != nil {
		log.Fatal().Err(err).Msgf("Cannot start %s", ServiceName)
	}

	aws_session, err := setupAwsSession()
	if err != nil {
		log.Fatal().Err(err).Msgf("Cannot start %s", ServiceName)
	}

	tunnel, err := localtunnel.Listen(localtunnel.Options{})
	if err != nil {
		log.Fatal().Err(err).Msgf("Cannot start %s", ServiceName)
	}
	log.Info().Msgf("Tunnel opened at '%s'", tunnel.URL())

	if err = saveTunnelUrlInParameterStore(aws_session, tunnel.URL()); err != nil {
		log.Fatal().Err(err).Msgf("Cannot start %s", ServiceName)
	}

	c := alice.New().
		Append(hlog.NewHandler(log)).
		Append(hlog.AccessHandler(func(r *http.Request, status, size int, duration time.Duration) {
			hlog.FromRequest(r).Info().
				Str("method", r.Method).
				Stringer("url", r.URL).
				Int("status", status).
				Int("size", size).
				Dur("duration", duration).
				Msg("")
		})).
		Append(hlog.RemoteAddrHandler("ip")).
		Append(hlog.UserAgentHandler("user_agent")).
		Append(hlog.RefererHandler("referer")).
		Append(hlog.RequestIDHandler("req_id", "Request-Id"))

	pingHandler := c.Then(http.HandlerFunc(ping))
	printHandler := c.Then(http.HandlerFunc(print))
	printerHandler := c.Then(http.HandlerFunc(printer))

	server := &http.Server{
		Addr:         "127.0.0.1:8080",
		WriteTimeout: 30 * time.Second,
		ReadTimeout:  30 * time.Second,
	}

	http.Handle("/ping", pingHandler)
	http.Handle("/print", printHandler)
	http.Handle("/printer", printerHandler)

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT)

	go func() {
		log.Info().Msg("Starting HTTP server")
		if err := server.Serve(tunnel); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("Server startup failed")
		}
	}()

	sig := <-sigs
	fmt.Println(sig)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Fatal().Err(err).Msgf("error when shutting down the main server %s", ServiceName)
	}

	log.Info().Msgf("%s service has shutdown", "my-service")
}

func saveTunnelUrlInParameterStore(aws_session *session.Session, tunnelURL string) error {
	log.Debug().
		Str("tunnelUrlParameterName", TunnelURLParameterName).
		Str("tunnelURL", tunnelURL).
		Msg("saveTunnelUrlInParameterStore")

	_, err := ssm.New(aws_session).PutParameter(&ssm.PutParameterInput{
		Name:      aws.String(TunnelURLParameterName),
		Value:     aws.String(tunnelURL),
		Type:      aws.String("String"),
		Overwrite: aws.Bool(true),
	})
	if err != nil {
		return fmt.Errorf("failed to save tunnel URL in AWS Parameter Store: %v", err.Error())
	}
	return err
}

func setupAwsSession() (*session.Session, error) {
	log.Debug().Msg("setupAwsSession")
	aws_session, err := session.NewSession(&aws.Config{Region: aws.String(Region)})
	if err != nil {
		return aws_session, fmt.Errorf("could not create AWS session: %v", err.Error())
	}
	return aws_session, err
}

func createUploadDirectory() error {
	err := os.MkdirAll(UploadDirectory, os.ModePerm)
	if err != nil {
		return fmt.Errorf("failed to create upload directory: %v", err.Error())
	}
	return nil
}

func (j PrintJob) print(logger zerolog.Logger) error {
	cmd := exec.Command("brother_ql",
		"--backend", "pyusb",
		"--model", j.Printer.Name,
		"--printer", j.Printer.Port,
		"print",
		"-l", j.FormatName,
		j.FilePath)

	output, err := cmd.CombinedOutput()

	logger.Debug().Msg(string(output))

	if err != nil {
		logger.Err(err).Msg("")
	}

	return err
}

func printerActive(logger zerolog.Logger, port string) (bool, error) {
	cmd := exec.Command("brother_ql",
		"--backend", "pyusb",
		"discover")

	output, err := cmd.CombinedOutput()

	logger.Debug().Msg(string(output))
	if err != nil {
		logger.Err(err).Msg("")
	}

	active := strings.ContainsAny(string(output), port)

	logger.Debug().Msgf("Printer active: %T", active)

	return active, err
}

func print(rw http.ResponseWriter, req *http.Request) {
	switch req.Method {
	case http.MethodPost:
		var labelImage LabelImage

		hlog.FromRequest(req).Debug().Msgf("Getting the label file from form")
		if err := labelImage.retrieveImageFromForm(rw, req); err != nil {
			hlog.FromRequest(req).Error().Err(err).Msg("")
			return
		}

		if err := labelImage.getPNGDimensions(rw); err != nil {
			hlog.FromRequest(req).Error().Err(err).Msg("")
			return
		}

		hlog.FromRequest(req).Info().
			Int("X", labelImage.Dimensions.X).
			Int("Y", labelImage.Dimensions.Y).
			Msg("Dimensions")

		format, exists := labelFormats[labelImage.Dimensions]
		if !exists {
			err := fmt.Errorf("dimensions %v is not valid, must match map %v", labelImage.Dimensions, labelFormats)
			http.Error(rw, err.Error(), http.StatusBadRequest)
			return
		}

		printJob := &PrintJob{
			Printer:    labelPrinters[format],
			FormatName: format.Name,
			FilePath:   labelImage.File.Name(),
		}

		hlog.FromRequest(req).Info().
			Str("PrinterName", printJob.Printer.Name).
			Str("PrinterPort", printJob.Printer.Port).
			Str("FormatName", printJob.FormatName).
			Str("FilePath", printJob.FilePath).
			Msg("Printing job")

		if err := printJob.print(hlog.FromRequest(req).With().Logger()); err != nil {
			hlog.FromRequest(req).Error().Err(err).Msg("")
			http.Error(rw, "something went wrong printing the label", http.StatusInternalServerError)
			return
		}

		err := os.Remove(labelImage.File.Name())
		if err != nil {
			hlog.FromRequest(req).Error().Err(err).Msg("could not delete the image after processing")
		}
	}
}

type PrinterResponse struct {
	Model  string `json:"model"`
	Active bool   `json:"active"`
	Label  string `json:"label"`
}

func printer(rw http.ResponseWriter, req *http.Request) {
	switch req.Method {
	case http.MethodGet:
		labelQueryParameterName := "label"

		if !req.URL.Query().Has(labelQueryParameterName) {
			hlog.FromRequest(req).Info().Msgf("Request must include 'label' query parameter")
			rw.WriteHeader(http.StatusBadRequest)
			return
		}

		requestedLabel := req.URL.Query().Get(labelQueryParameterName)

		response := &PrinterResponse{
			Active: false,
			Label:  requestedLabel,
		}

		var printer Printer

		for _, format := range labelFormats {
			if format.Name != requestedLabel {
				continue
			}

			printer = labelPrinters[format]
			break
		}

		hlog.FromRequest(req).Debug().
			Str("Model", printer.Name).
			Str("Label", printer.Port).
			Msgf("Printer")

		response.Model = printer.Name

		if response.Model == "" {
			hlog.FromRequest(req).Info().Msgf("Model for label '%s' not found", requestedLabel)
			rw.WriteHeader(http.StatusNotFound)
			return
		}

		active, err := printerActive(hlog.FromRequest(req).With().Logger(), printer.Port)
		if err != nil {
			hlog.FromRequest(req).Err(err).Msgf("")
			rw.WriteHeader(http.StatusInternalServerError)
			return
		}

		response.Active = active

		hlog.FromRequest(req).Debug().
			Str("Model", response.Model).
			Str("Label", response.Label).
			Bool("Active", response.Active).
			Msgf("Response object")

		responseBytes, err := json.Marshal(response)
		if err != nil {
			hlog.FromRequest(req).Err(err).Msgf("")
			rw.WriteHeader(http.StatusInternalServerError)
			return
		}

		if _, err := rw.Write(responseBytes); err != nil {
			hlog.FromRequest(req).Err(err).Msgf("")
			rw.WriteHeader(http.StatusInternalServerError)
			return
		}
	}
}

func ping(rw http.ResponseWriter, req *http.Request) {
	hlog.FromRequest(req).Info().Msg("ping")
	switch req.Method {
	case http.MethodGet:
		if _, err := rw.Write([]byte("pong\n")); err != nil {
			hlog.FromRequest(req).Debug().Msgf("error when writing response for /ping request")
			rw.WriteHeader(http.StatusInternalServerError)
		}
	}
}

func (l *LabelImage) retrieveImageFromForm(rw http.ResponseWriter, req *http.Request) error {
	hlog.FromRequest(req).Debug().Msgf("Checking size < 10MB")
	if err := req.ParseMultipartForm(10 << 20); err != nil {
		err = fmt.Errorf("upload should be fewer than 10MB: %w", err)
		http.Error(rw, err.Error(), http.StatusInternalServerError)
		return err
	}

	hlog.FromRequest(req).Debug().Msgf("Retrieving image from form")
	file, header, err := req.FormFile("image")
	if err != nil {
		err = fmt.Errorf("form file not found at key 'image': %w", err)
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return err
	}
	defer file.Close()

	filePath := filepath.Join(UploadDirectory, header.Filename)
	out, err := os.Create(filePath)
	if err != nil {
		err = fmt.Errorf("unable to create file for copying the form image: %w", err)
		http.Error(rw, err.Error(), http.StatusInternalServerError)
		return err
	}
	defer out.Close()

	hlog.FromRequest(req).Debug().Msgf("copying file: %s", out.Name())
	_, err = io.Copy(out, file)
	if err != nil {
		err = fmt.Errorf("unable to copy form content to file: %w", err)
		http.Error(rw, err.Error(), http.StatusInternalServerError)
		return err
	}

	l.File = out

	return nil
}

type LabelImage struct {
	File       *os.File
	Dimensions LabelDimensions
}

func (l *LabelImage) getPNGDimensions(rw http.ResponseWriter) error {
	file, err := os.Open(l.File.Name())
	if err != nil {
		err = fmt.Errorf("could not get image from file: %w", err)
		http.Error(rw, err.Error(), http.StatusInternalServerError)
		return err
	}
	defer file.Close()

	img, err := png.Decode(file)
	if err != nil {
		err = fmt.Errorf("could not decode file to PNG: %w", err)
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return err
	}

	bounds := img.Bounds()
	l.Dimensions.X = bounds.Dx()
	l.Dimensions.Y = bounds.Dy()

	return nil
}
