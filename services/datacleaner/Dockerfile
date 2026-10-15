FROM golang:1.26-alpine AS builder

WORKDIR /workspace

# Copy go module files and common dependency
COPY services/datacleaner/go.mod services/datacleaner/go.sum ./
COPY services/common ../common

RUN go mod download

# Copy datacleaner source code
COPY services/datacleaner .

RUN CGO_ENABLED=0 GOOS=linux go build -o /workspace/datacleaner .


FROM alpine:latest

RUN apk add --no-cache ca-certificates && \
    addgroup -S appuser && \
    adduser -S appuser -G appuser

COPY --from=builder /workspace/datacleaner /datacleaner

USER appuser

ENTRYPOINT ["/datacleaner"]
