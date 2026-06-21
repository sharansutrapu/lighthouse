# Contributing to LightHouse

First off, thank you for considering contributing to LightHouse! We welcome contributions from everyone—whether it's reporting a bug, proposing a new feature, or submitting a Pull Request.

## How Can I Contribute?

### 1. Reporting Bugs
Before creating bug reports, please check the existing issues to see if the problem has already been reported. When you create a bug report, please include as many details as possible:
* Use a clear and descriptive title.
* Describe the exact steps to reproduce the problem.
* Provide specific examples or logs if applicable.
* Describe the behavior you observed after following the steps and point out what exactly is the problem.
* Explain which behavior you expected to see instead.

### 2. Suggesting Enhancements
Enhancement suggestions are tracked as GitHub issues. When creating an enhancement suggestion, please:
* Use a clear and descriptive title.
* Provide a step-by-step description of the suggested enhancement.
* Explain why this enhancement would be useful to most LightHouse users.

### 3. Pull Requests
1. **Fork the repo** and create your branch from `main`.
2. **If you've added code** that should be tested, add tests.
3. **Ensure the test suite passes**. Run `go test ./...` in the backend directory.
4. **Update documentation** if you are modifying user-facing features (check `README.md` and `docs/`).
5. **Issue that PR!** We will review it as soon as we can.

## Local Development Setup

### Prerequisites
* Go 1.20+
* Node.js 18+ and npm
* Docker

### Backend (Go)
1. Navigate to the root directory.
2. Install dependencies: `go mod tidy`
3. Run the development server: `go run main.go`

### Frontend (Vue 3)
1. Navigate to the `frontend` directory.
2. Install dependencies: `npm install`
3. Run the dev server: `npm run dev`

By default, the backend runs on `http://localhost:8000` and the frontend proxy targets it automatically if configured via Vite.

## Pull Request Guidelines

- We use standard [Go formatting tools](https://golang.org/cmd/gofmt/). Please format your Go code before submitting.
- Follow Vue 3 best practices for the frontend.
- Keep your commits atomic and your commit messages descriptive.

Thank you for contributing to LightHouse!
