FROM golang:1.25-alpine3.22@sha256:d3f0cf7723f3429e3f9ed846243970b20a2de7bae6a5b66fc5914e228d831bbb AS builder

WORKDIR /app
COPY go.* ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o lightsout .

FROM alpine:3.22@sha256:4b7ce07002c69e8f3d704a9c5d6fd3053be500b7f1c69fc0d80990c2ad8dd412
RUN apk --no-cache add ca-certificates curl docker-cli
WORKDIR /root/

COPY --from=builder /app/lightsout .

# Set default environment variables
ENV PORT=8808
ENV INACTIVITY_TIMEOUT=90
ENV LOG_LEVEL=INFO
ENV GOOGLE_PROJECT_ID=""
ENV GCE_ZONE=""
ENV GCE_INSTANCE=""
ENV LIBOPS_KEEP_ONLINE=""

EXPOSE 8808

CMD ["/app/lightsout"]
