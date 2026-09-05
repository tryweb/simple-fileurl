# syntax=docker/dockerfile:1

FROM golang:1.24-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
COPY main.go ./
COPY internal/ ./internal/
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/fileurl .

FROM alpine:3.23
# WEB_GID owns per-user directories (mode 0750) so the non-root web process
# can read them: the app user carries it as a supplementary group. Compose
# also passes group_add so a runtime WEB_GID differing from the build arg
# still grants read access.
ARG WEB_GID=2001
RUN apk add --no-cache ca-certificates \
    && addgroup -S -g ${WEB_GID} webreaders \
    && addgroup -S app && adduser -S -G app app \
    && addgroup app webreaders \
    && mkdir -p /var/lib/file-links \
    && chown app:app /var/lib/file-links
COPY --from=builder /out/fileurl /usr/local/bin/fileurl
USER app
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/fileurl"]
