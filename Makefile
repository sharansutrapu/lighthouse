.PHONY: dev build install clean

dev:
	@echo "Starting development server..."
	@go run . & pnpm run --prefix frontend dev

build:
	@echo "Building frontend..."
	@pnpm install --prefix frontend --frozen-lockfile
	@pnpm run --prefix frontend build
	@echo "Building backend..."
	@go build -o lighthouse .

install: build
	@echo "Installing lighthouse to /usr/local/bin (may require sudo)..."
	@install -m 755 lighthouse /usr/local/bin/lighthouse
	@echo "Installed: lighthouse (run 'lighthouse help')"

docker-build:
	@echo "Building Docker image..."
	@docker build -t lighthouse:latest .

up:
	@echo "Starting LightHouse and test containers..."
	@touch lighthouse.db
	@docker-compose up --build -d

down:
	@echo "Stopping containers..."
	@docker-compose down

clean:
	@rm -rf lighthouse frontend/dist
	@echo "Cleaned up."
