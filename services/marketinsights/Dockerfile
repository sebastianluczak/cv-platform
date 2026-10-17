FROM alpine:3.22

RUN apk add --no-cache ca-certificates \
    && addgroup -S app \
    && adduser -S app -G app \
    && mkdir -p /data \
    && chown app:app /data

WORKDIR /app

COPY marketinsights ./marketinsights

USER app

EXPOSE 8084

ENTRYPOINT ["./marketinsights"]
