.PHONY: run test race fmt vet build docker-up docker-down

run:
	go run ./cmd/api

test:
	go test ./...

race:
	go test -race ./...

fmt:
	gofmt -w ./cmd ./internal

vet:
	go vet ./...

build:
	go build -o bin/neighborparking ./cmd/api

docker-up:
	docker compose up --build

docker-down:
	docker compose down
