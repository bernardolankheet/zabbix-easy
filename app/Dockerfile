FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY . .
RUN apk add --no-cache git
RUN go mod tidy
RUN go build -o go-zabbix-app ./cmd/app

FROM alpine:latest
WORKDIR /root/
COPY --from=builder /app/go-zabbix-app .
COPY web ./web
EXPOSE 8080
CMD ["./go-zabbix-app"]