# Dockerfile para Go Zabbix HealthCheck Report
FROM golang:1.21-alpine as builder
WORKDIR /app
COPY . .
RUN cd cmd/app && go build -o /app/app

FROM alpine:3.18
WORKDIR /app
COPY --from=builder /app/app /app/app
COPY web /app/web
EXPOSE 8080
CMD ["/app/app"]
