FROM otiai10/gosseract:latest

USER app

WORKDIR /app

COPY extractor ./extractor

ENTRYPOINT ["./extractor"]