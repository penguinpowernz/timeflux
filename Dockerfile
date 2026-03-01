FROM golang:1.26-alpine AS builder

WORKDIR /build

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o timeflux .

# Final stage
FROM alpine:latest

RUN apk --no-cache add ca-certificates

WORKDIR /app

# Copy binary from builder
COPY --from=builder /build/timeflux .

# Copy example config (will be overridden by volume mount)
COPY config.yaml.example ./config.yaml

EXPOSE 8086

CMD ["./timeflux", "-config", "/app/config.yaml"]
