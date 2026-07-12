.PHONY: build test lint docker docs-dev docs-build run

build:
	go build -o bin/cachepotd ./cmd/cachepotd

run: build
	./bin/cachepotd --port 6380

test:
	go test ./... -race

lint:
	golangci-lint run

docker:
	docker build -f deployments/docker/Dockerfile -t cache-pot:dev .

docs-dev:
	cd docs && npm install && npm run docs:dev

docs-build:
	cd docs && npm install && npm run docs:build
