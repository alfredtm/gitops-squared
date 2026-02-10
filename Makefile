.PHONY: setup teardown build run-api demo test clean port-forward

setup:
	./scripts/setup.sh

teardown:
	./scripts/teardown.sh

build:
	go build -o bin/api ./cmd/api

run-api:
	go run ./cmd/api

port-forward:
	kubectl -n gitops-squared port-forward svc/api 8080:8080

demo:
	./scripts/demo.sh

test:
	go test ./...

clean:
	rm -rf bin/
