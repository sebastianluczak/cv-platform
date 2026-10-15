FROM alpine:3.22

RUN apk add --no-cache ca-certificates \
    && addgroup -S app \
    && adduser -S app -G app

WORKDIR /app

COPY offermatcher ./offermatcher

USER app

EXPOSE 8081

ENTRYPOINT ["./offermatcher"]
