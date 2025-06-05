.PHONY: run
run:
	go run ./cmd/frigg/main.go

.PHONY: test
test:
	go test -coverprofile=cover.out -shuffle on -short ./...
