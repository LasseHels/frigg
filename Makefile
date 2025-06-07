.PHONY: run
run:
	go run ./cmd/frigg/main.go

.PHONY: test
test:
	go test -coverprofile=cover.out -shuffle on -short ./...

.PHONY: test-all
test-all:
	go test -coverprofile=cover.out -shuffle on ./...

.PHONY: lint
lint:
	golangci-lint run --fix
