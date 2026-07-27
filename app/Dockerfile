FROM golang:1.22-alpine AS builder
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o go-zabbix-app ./cmd/app

FROM alpine:latest
# Certificados copiados do builder — evita apk add (falha sem internet/registry)
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
WORKDIR /root/
COPY --from=builder /build/go-zabbix-app ./go-zabbix-app
COPY web ./web
EXPOSE 8080
CMD ["./go-zabbix-app"]
