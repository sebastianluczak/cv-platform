FROM otiai10/gosseract:latest

USER root

RUN apt-get update \
    && apt-get install -y --no-install-recommends poppler-utils \
    && rm -rf /var/lib/apt/lists/*

USER app

WORKDIR /app

COPY extractor ./extractor

ENTRYPOINT ["./extractor"]