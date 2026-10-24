# Build
FROM golang:1.27.3-alpine AS build
WORKDIR /src
RUN apk add --no-cache ca-certificates
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/pinacoteca ./cmd/pinacoteca

# Runtime
FROM alpine:3.24
RUN apk add --no-cache ca-certificates && adduser -D -H -u 1000 pinacoteca
COPY --from=build /out/pinacoteca /usr/local/bin/pinacoteca
USER pinacoteca
VOLUME ["/data"]
EXPOSE 8080
ENTRYPOINT ["pinacoteca"]
CMD ["serve", "--data", "/data", "--addr", ":8080"]
