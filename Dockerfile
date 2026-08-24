# Build stage
FROM golang:1.27-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o aibutler ./main.go

# Runtime stage
FROM alpine:3.24
RUN apk add --no-cache ca-certificates tzdata ffmpeg && \
    addgroup -S aibutler && adduser -S aibutler -G aibutler
WORKDIR /app
COPY --from=builder /app/aibutler /usr/local/bin/aibutler
RUN mkdir -p /data && chown aibutler:aibutler /data
ENV AIBUTLER_DATA=/data
USER aibutler
EXPOSE 8080 8081
HEALTHCHECK --interval=30s --timeout=5s --retries=3 CMD wget -q --spider http://localhost:8080/healthz || exit 1
ENTRYPOINT ["aibutler"]
CMD ["run"]
