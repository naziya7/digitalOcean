# Build stage
FROM golang:1.27-alpine AS builder

WORKDIR /src

RUN apk add --no-cache git ca-certificates

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/server ./cmd/server

# Runtime stage
FROM alpine:3.20

RUN apk add --no-cache ca-certificates wget \
	&& adduser -D -u 10001 appuser

WORKDIR /app

COPY --from=builder /out/server /app/server

RUN mkdir -p /app/data/originals /app/data/thumbnails \
	&& chown -R appuser:appuser /app

USER appuser

ENV HTTP_PORT=8080 \
	DATA_DIR=/app/data \
	MONGO_URI=mongodb://mongo:27017 \
	MONGO_DATABASE=thumbnails

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
	CMD wget -qO- http://127.0.0.1:8080/healthz || exit 1

ENTRYPOINT ["/app/server"]
