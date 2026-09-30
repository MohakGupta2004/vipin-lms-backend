.PHONY: build run
-include .env
build:
	@go build -o bin/api ./cmd/api

run: build
	@./bin/api

migrate-up:
	@migrate -path ./internal/database/migrations -database ${DATABASE_URL} up

migrate-down:
	@migrate -path ./internal/database/migrations -database ${DATABASE_URL} down 1

migrate-force:
	@migrate -path ./internal/database/migrations -database ${DATABASE_URL} force $(VERSION)

air:
	@air --build.cmd "go build -o bin/api cmd/api/main.go" --build.entrypoint "./bin/api"
