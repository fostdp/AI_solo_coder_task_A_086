FROM golang:1.21-alpine AS builder
WORKDIR /src
RUN apk add --no-cache git ca-certificates tzdata
ENV CGO_ENABLED=0 GOOS=linux GOARCH=amd64
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -ldflags="-s -w -extldflags '-static'" -o /bin/zhaozhou-bridge-api .

FROM alpine:3.19
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
ENV TZ=Asia/Shanghai
COPY --from=builder /bin/zhaozhou-bridge-api /app/zhaozhou-bridge-api
COPY config/bridge_config.json /app/config/bridge_config.json
COPY frontend /app/frontend
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -qO- http://localhost:8080/health || exit 1
ENTRYPOINT ["/app/zhaozhou-bridge-api"]
CMD ["--config", "/app/config/bridge_config.json", "--db", "postgres://bridge_admin:bridge2024@timescaledb:5432/zhaozhou_bridge?sslmode=disable", "--mqtt", "mosquitto:1883"]
