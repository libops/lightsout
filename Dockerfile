FROM golang:1.27.2-alpine@sha256:f92b6ef800e499660581efdabdf25d9d817a9d124eaf900924f0504e7e27e12d AS builder

SHELL ["/bin/ash", "-o", "pipefail", "-ex", "-c"]

WORKDIR /app

COPY go.* ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY *.go ./

RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -ldflags="-s -w" -o /app/binary .

FROM alpine:3.24@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6

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
