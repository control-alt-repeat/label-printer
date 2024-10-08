FROM golang:1.23-alpine as builder

WORKDIR /app

COPY main.go go.mod go.sum vendor/ ./

RUN go build -mod=readonly -o /app/label-printer

FROM python:3.6-alpine

WORKDIR /app

COPY --from=builder /app/label-printer /app/label-printer
COPY cat-62x100.png /app/

RUN apk update
RUN apk add --no-cache libusb-dev zlib zlib-dev jpeg-dev gcc musl-dev

RUN pip install brother_ql pyusb
RUN brother_ql

ENTRYPOINT [ "./label-printer" ]
