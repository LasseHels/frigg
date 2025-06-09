.PHONY: run
run:
	go run -ldflags="-X 'main.release=`git rev-parse --short=8 HEAD`'" ./cmd/frigg/main.go

.PHONY: test
test:
	go test -coverprofile=cover.out -shuffle on -short ./...

.PHONY: test-all
test-all:
	go test -coverprofile=cover.out -shuffle on ./...

.PHONY: lint
lint:
	golangci-lint run --fix
