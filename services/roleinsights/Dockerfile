FROM alpine:3.22

RUN apk add --no-cache ca-certificates \
    && addgroup -S app \
    && adduser -S app -G app

WORKDIR /app

COPY roleinsights ./roleinsights

USER app

EXPOSE 8083

ENTRYPOINT ["./roleinsights"]
