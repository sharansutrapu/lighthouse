# Stage 1: Build the Vue frontend
FROM --platform=$BUILDPLATFORM node:24-alpine3.24 AS frontend-builder
WORKDIR /app/frontend
COPY frontend/package.json frontend/pnpm-lock.yaml ./
RUN npm install -g "$(node -p 'require("./package.json").packageManager')"
RUN pnpm install --frozen-lockfile
COPY frontend/ ./
RUN pnpm run build

# Stage 2: Build the Go backend (build entire package, not main.go alone)
FROM --platform=$BUILDPLATFORM golang:1.26.11-alpine3.24 AS backend-builder
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
FROM alpine:3.24
WORKDIR /app
RUN apk upgrade --no-cache && \
    apk add --no-cache ca-certificates docker-cli docker-cli-compose git "libcrypto3>=3.5.9-r1" "libssl3>=3.5.9-r1"
COPY --from=backend-builder /app/lighthouse /usr/local/bin/lighthouse
COPY --from=backend-builder /app/frontend/dist ./frontend/dist

EXPOSE 8000
CMD ["lighthouse", "server"]
