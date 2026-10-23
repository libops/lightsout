FROM golang:1.27.3-alpine@sha256:b8c9b82affbbb82a020bbcf998dad2ba9516944ac7e883524642a481a9505e9c AS builder

SHELL ["/bin/ash", "-o", "pipefail", "-ex", "-c"]

WORKDIR /app

COPY go.* ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY *.go ./

RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -ldflags="-s -w" -o /app/binary .

FROM alpine:3.24@sha256:be2656061b536c1883c4034790e21d0a09c5aaaf51968b89368a3c0371ea2c9a

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
