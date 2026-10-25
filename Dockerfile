# Stage 1: Build
FROM golang:1.26-alpine AS builder

WORKDIR /app

# proxy.golang.org is unreachable from mainland China; use a mirror by default.
# Override with: docker build --build-arg GOPROXY=https://your-proxy,direct
RUN go env -w GOPROXY=https://goproxy.cn,direct

# Cache dependencies
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Build binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o main ./cmd/server/main.go

# Stage 2: Final Image
FROM alpine:latest

# Install required packages
RUN apk add --no-cache ca-certificates tzdata

# Create a non-root user and group
RUN addgroup -S appgroup && adduser -S appuser -G appgroup

WORKDIR /home/appuser

# Runtime writes accounts (data/db) and generated images (data/images).
# Pre-create them with the appuser owner so the image also works without bind mounts.
RUN mkdir -p data/db data/images .cookies && chown -R appuser:appgroup data .cookies

# Generated image folders use yyyyMMdd from local time; without TZ the container
# defaults to UTC and would create folders for the wrong day.
ENV TZ=Asia/Shanghai

# Copy file from builder and change ownership
COPY --from=builder --chown=appuser:appgroup /app/main .

# Switch to non-root user
USER appuser

EXPOSE 4981

ENTRYPOINT ["./main"]
