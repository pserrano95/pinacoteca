.PHONY: verify fmt vet test docker-build

verify: fmt vet test docker-build

fmt:
	@test -z "$$(gofmt -l .)" || (echo "gofmt needed on:" && gofmt -l . && exit 1)

vet:
	go vet ./...

test:
	go test ./...

docker-build:
	docker build -t pinacoteca:local .
