FROM golang:1.27.1-alpine@sha256:cf6fca6641884b8433441b2b0652976f975e1d0fdd26d177eaaf8596087f3125 AS builder

SHELL ["/bin/ash", "-o", "pipefail", "-ex", "-c"]

WORKDIR /app

COPY go.* ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY *.go ./

RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -ldflags="-s -w" -o /app/binary .

FROM alpine:3.24@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b

RUN apk add --no-cache ca-certificates

COPY --from=builder /app/binary /app/binary

USER 65532:65532

ENV \
    PORT=8808 \
    INACTIVITY_TIMEOUT=90 \
    LOG_LEVEL=INFO \
    GCP_PROJECT= \
    GCP_ZONE= \
    GCP_INSTANCE_NAME= \
    LIBOPS_KEEP_ONLINE=

CMD ["/app/binary"]
