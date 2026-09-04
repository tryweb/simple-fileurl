# syntax=docker/dockerfile:1

FROM golang:1.24-alpine AS builder
WORKDIR /src
COPY go.mod ./
COPY main.go ./
COPY internal/ ./internal/
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/fileurl .

FROM alpine:3.23
RUN apk add --no-cache ca-certificates \
    && addgroup -S app && adduser -S -G app app
COPY --from=builder /out/fileurl /usr/local/bin/fileurl
USER app
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/fileurl"]
