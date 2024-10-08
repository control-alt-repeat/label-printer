FROM golang:1.23-alpine as builder

WORKDIR /app

COPY main.go go.mod go.sum vendor/ ./

RUN go build -mod=readonly -o /app/label-printer

FROM python:3.6-alpine

WORKDIR /app

COPY --from=builder /app/label-printer /app/label-printer

RUN apk update
RUN apk add --no-cache libusb zlib zlib-dev jpeg-dev gcc musl-dev

RUN pip install brother_ql
RUN brother_ql

ENTRYPOINT [ "./label-printer" ]
