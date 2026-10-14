# Copilot instructions for cv-platform

## Repository overview

This repository is a small monorepo for a CV-processing platform:

- `ui/` contains the browser UI, built with React + Vite + TypeScript.
- `services/` contains the backend microservices in Go:
  - `uploader`: accepts uploads, stores files in the shared object store, publishes RabbitMQ jobs, and serves SSE status + results endpoints.
  - `extractor`: consumes jobs, extracts text from PDFs/images, writes `results/<job-id>.txt`, and publishes the next stage.
  - `piistripper`: redacts the extraction result and publishes the skill-matching job.
  - `skillmatcher`: reads the redacted text, scores the resume, and writes `results/<job-id>.skills.json`.
- `docs/` contains the Antora documentation site for the architecture and operational design.
- `reviewer/` contains a Python helper for AI-assisted code review, not the main runtime.

The runtime architecture is asynchronous and queue-driven: browser UI -> uploader HTTP endpoint -> RabbitMQ queue -> extractor -> piistripper -> skillmatcher -> shared S3-compatible bucket. The uploader exposes `/events?job_id=<id>` as SSE and `/results?job_id=<id>` for completion data.

## Build, test, and lint commands

### Local stack / full dev environment

Use the project defaults from the root when you need the full stack running:

```bash
cp .env.dist .env
# adjust values in .env as needed

devenv up
```

This boots the local dev environment and starts the services plus the frontend unless the environment is running in test mode.

### UI commands

```bash
cd ui
npm ci
npm run dev
npm run build
npm run lint
```

The frontend uses Vite and ships with `oxlint` for linting. There is no dedicated Jest/Vitest suite in the current setup; the primary validation path is a successful build plus lint.

### Go service validation

The repo has Go tests per service, and they are intentionally run module-by-module rather than from the root:

```bash
# single service test
cd services/uploader && go test -run 'TestName' -v
cd services/extractor && go test -run 'TestName' -v
cd services/piistripper && go test -run 'TestName' -v
cd services/skillmatcher && go test -run 'TestName' -v

# full backend suite defined by the repo
sh test.sh
# or explicitly:
go -C services/uploader test -v
go -C services/extractor test -v
go -C services/piistripper test -v
go -C services/skillmatcher test -v
```

The root `test.sh` script is the authoritative backend test entry point. `devenv test` also runs it.

### Formatting and repo lint helpers

```bash
# gofmt across every Go service
task prep:lint
```

This runs `gofmt -l -e -w .` in each service directory.

### Docs build

```bash
cd docs
npm ci
npx antora --fetch antora-playbook.yml
```

The repo also exposes this through `task docs:build-docs`.

## Architecture and runtime contracts

### Browser to backend flow

- The UI runs separately from the backend and proxies `/api` requests to the uploader service in development (`ui/vite.config.ts`).
- The UI calls `/api/upload`, `/api/events?job_id=<job-id>`, and `/api/results?job_id=<job-id>`; these are proxied to the uploader's HTTP handlers.
- This is a queue-backed pipeline rather than a synchronous request/response workflow.

### Shared infrastructure

The platform expects these shared resources to be configured consistently across services:

- RabbitMQ URL (`RABBITMQ_URL`)
- queue name (`QUEUE_NAME`), usually a shared prefix like `cv-platform-queue`
- S3-compatible bucket config (`RUSTFS_*` variables)

The docs describe the project as relying on:

- RabbitMQ for durable jobs and status events
- S3-compatible storage (RustFS in local dev) for uploads and results
- SSE for live client progress updates

### Processing contract

The implementation is deliberately at-least-once and retry-based:

- Job IDs are UUIDs and are used as deterministic object keys.
- Objects are stored with predictable names such as:
  - `uploads/<job-id>`
  - `results/<job-id>.txt`
  - `results/<job-id>.redacted.txt`
  - `results/<job-id>.skills.json`
- The pipeline uses durable queues and manual acknowledgements with publisher confirms; failures are retried and eventually routed to `*.failed` queues.
- Status events are published to `<QUEUE_NAME>.events`; the uploader retains only the latest in-memory snapshot for SSE clients.

This matters when making changes: if you alter the queue contract, object keys, or accepted job shapes, update the dependent service contracts together.

## Key conventions and repo-specific patterns

- Keep `RABBITMQ_URL`, `QUEUE_NAME`, and `RUSTFS_*` settings aligned across uploader/extractor/piistripper/skillmatcher; they are expected to be shared.
- Prefer deterministic object keys and UUID job IDs; the pipeline treats repeated writes as safe due to key determinism.
- Do not assume a single-process runtime. The uploader keeps SSE state in memory, so a restart clears that in-memory status; the repo explicitly notes this design limitation.
- Use `gofmt` and the repo's `task prep:lint` target for Go changes rather than ad hoc formatting.
- Keep the frontend and backend loosely coupled: the UI should consume the uploader's HTTP contract rather than reimplementing backend logic.
- Treat architecture docs in `docs/cv-analyzer/modules/architecture/` as the source of truth for workflow semantics and queue semantics when the code and docs disagree.

## Practical editing guidance

When working in this repo, prefer changes that preserve the queue-based, asynchronous pipeline:

- If modifying upload / result endpoints, keep the `uploader` and client contract aligned.
- If altering queue names or message patterns, update all participating services in the same change set.
- If adding new status states, ensure the uploader SSE status handlers and any docs/consumer code are updated consistently.
- For Go code, keep changes in the service module boundaries and test the relevant module directly.

## Defensive notes

- The docs state that the system is “at-least-once,” not exactly-once. Do not introduce code that assumes a single delivery or deduplicated execution.
- The uploader's in-memory status store is intentionally transient; do not rely on it as durable history across restarts.
- The skill matcher is heuristic and deterministic; it scores resume text patterns rather than evaluating candidate quality in a subjective sense.
