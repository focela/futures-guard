# Dockerfile for Futures Guard - Binance futures trading position manager
# Multi-stage build for optimized production container

# --- Build stage ---
FROM golang:1.24-alpine AS builder

# Set working directory
WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git ca-certificates tzdata

# Copy and download Go dependencies first (for better layer caching)
COPY go.mod go.sum ./
RUN go mod download && go mod verify

# Copy source code
COPY . .

# Build the binary with optimizations
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-w -s" -o futures-guard main.go

# --- Runtime stage ---
FROM alpine:3.19

# Add metadata labels
LABEL maintainer="Focela" \
      description="Binance futures trading position manager with automated stop-loss and take-profit"

# Create non-root user for security
RUN addgroup -S appgroup && adduser -S appuser -G appgroup

# Set working directory
WORKDIR /app

# Install runtime dependencies
RUN apk add --no-cache ca-certificates tzdata curl

# Copy the binary from builder stage
COPY --from=builder /app/futures-guard /app/

# Copy the example env file for reference (but don't use it directly)
COPY .env.example /app/

# Create volume for actual .env file that should be mounted at runtime
VOLUME ["/app/config"]

# Set proper permissions
RUN chown -R appuser:appgroup /app
USER appuser

# Set up healthcheck to verify the service is running properly
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD curl -f http://localhost:8080/health || exit 1

# Document exposed port if there's any web interface
# EXPOSE 8080

# Command to run the executable with env file from mounted volume
CMD ["sh", "-c", "cp -n /app/config/.env /app/.env 2>/dev/null || true && ./futures-guard"]
