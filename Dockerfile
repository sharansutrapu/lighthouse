# Stage 1: Build the Vue frontend
FROM node:20-alpine AS frontend-builder
WORKDIR /app/frontend
RUN npm install -g pnpm
COPY frontend/package.json frontend/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY frontend/ ./
RUN pnpm run build

# Stage 2: Build the Go backend (build entire package, not main.go alone)
FROM golang:1.26-alpine AS backend-builder
WORKDIR /app
COPY go.mod go.sum ./
RUN GOTOOLCHAIN=local go mod download
COPY . .
COPY --from=frontend-builder /app/frontend/dist ./frontend/dist

ARG TARGETOS=linux
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    GOTOOLCHAIN=local GOGC=50 GOMAXPROCS=2 \
    go build -p 2 -ldflags="-s -w" -o lighthouse .

# Stage 3: Final runtime image
FROM alpine:3.21
WORKDIR /app
RUN apk add --no-cache ca-certificates docker-cli docker-cli-compose git
COPY --from=backend-builder /app/lighthouse /usr/local/bin/lighthouse
COPY --from=backend-builder /app/frontend/dist ./frontend/dist

EXPOSE 8000
CMD ["lighthouse", "server"]
