FROM golang:1.19-alpine as builder

WORKDIR /app

COPY main.go go.mod go.sum vendor/ ./

RUN go build -mod=readonly -o /app/label-printer

FROM python:3.6

WORKDIR /app

COPY --from=builder /app/label-printer /app/label-printer

RUN pip install brother_ql

ENTRYPOINT [ "./label-printer" ]
