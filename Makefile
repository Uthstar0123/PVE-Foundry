.PHONY: build test lint run-server run-runner dev-web

build:
	go build ./cmd/server
	go build ./cmd/runner
	npm --prefix web run build

test:
	go test ./...
	npm --prefix web test

lint:
	go vet ./...
	npm --prefix web run lint

run-server:
	go run ./cmd/server

run-runner:
	go run ./cmd/runner

dev-web:
	npm --prefix web run dev
