---
name: go-cv-pipeline-domain
description: Apply the CV platform's Go service and asynchronous pipeline contracts when changing backend behavior.
---

# Go CV pipeline domain

Use this skill for Go changes that affect upload processing, service handoffs, status events, or stored results. Scope it to `services/`; it does not define UI or general product behavior.

## Pipeline contracts

- The pipeline is uploader → extractor → piistripper → skillmatcher, coordinated through RabbitMQ and shared S3-compatible storage. Read the relevant architecture pages under `docs/cv-analyzer/modules/architecture/` before changing workflow semantics.
- Processing is at-least-once. Use job IDs and the established deterministic object keys so repeated processing remains safe; do not assume exactly-once delivery.
- Preserve the handoff order: persist stage output, publish the next-stage job with confirmation, then acknowledge the current message. Keep the existing retry limit and failed-queue behavior consistent across affected consumers.
- Keep shared `RABBITMQ_URL`, `QUEUE_NAME`, and `RUSTFS_*` configuration and message shapes aligned across participating services. Update every affected producer, consumer, test, and relevant documentation when a contract changes.
- Status events use `<QUEUE_NAME>.events`; uploader progress snapshots are in-memory and are not durable history.
- Skill matching is deterministic and heuristic. Do not describe or change its scores as an objective assessment of a candidate.

## Validation

Test every affected Go module, especially producers and consumers on both sides of a changed handoff. Use `sh test.sh` when a change crosses service boundaries.
