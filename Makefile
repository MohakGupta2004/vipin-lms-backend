.PHONY: build run
-include .env
build:
	@go build -o bin/api ./cmd/api

run: build
	@./bin/api

migrate-up:
	@migrate -path ./internal/database/migrations -database ${DATABASE_URL} up 1 force 2

migrate-down:
	@migrate -path ./internal/database/migrations -database ${DATABASE_URL} down 1