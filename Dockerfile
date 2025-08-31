FROM golang:1.25-alpine3.22 AS builder

WORKDIR /app
COPY go.* ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o lightsout .

FROM alpine:3.22
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
