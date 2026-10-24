.PHONY: verify fmt vet test check-runtime-alpine docker-build

verify: fmt vet test check-runtime-alpine docker-build

fmt:
	@test -z "$$(gofmt -l .)" || (echo "gofmt needed on:" && gofmt -l . && exit 1)

vet:
	go vet ./...

test:
	go test ./...

# Refuse a runtime FROM whose Alpine branch no longer receives security
# fixes. The build stage (golang:…-alpine) is not the image that ships.
check-runtime-alpine:
	sh scripts/check-runtime-alpine.sh

docker-build:
	docker build -t pinacoteca:local .
