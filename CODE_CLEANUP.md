# Gradual Go Services Refinement Backlog

## Problem and approach

Prepare an actionable, junior-friendly improvement path for all nine Go modules
under `services/`, including the shared `common` module and supporting services.
Start with small, reversible service-local fixes and real tests. Only then take
on queue-contract changes, shared worker refactors, retention behavior, or
durable status architecture. Preserve the documented at-least-once workflow:
persist stage output, publish the next-stage job with confirmation, then
acknowledge the current message.

This is a backlog, not authorization to change runtime behavior. Where a ticket
could affect retries, queue handling, retention, or externally visible
contracts, first add tests and keep the existing contract unless its owner
approves a deliberate change.

## Current-state pain points

### Easy and local

- **Metrics configuration drift:** uploader, extractor, piistripper, skillmatcher,
  offermatcher, and datacleaner all start the metrics server using
  `METRICS_PORT_AIWEIGHTER`, even though the environment configuration defines
  service-specific ports. This can send multiple services to one port or leave
  the intended ports unused. Relevant entry points include
  `services/uploader/uploader.go`, `services/extractor/consumer.go`, and the
  other services' `main.go`.
- **Generated artifact in source tree:** `services/roleinsights/roleinsights`
  is a tracked ELF executable alongside the Go sources. Confirm no workflow
  relies on it, then remove it from version control and ignore build output.

### Reliability and test quality

- **Datacleaner behavior is difficult to trust:** `main.go` has a stale TODO
  saying RabbitMQ cleanup is unimplemented, although it calls
  `cleanupFailedQueues`. The queue list in `rabbitmq_cleanup.go` omits the
  documented `roleinsights.failed` queue. Cleanup errors are logged but
  `CleanupRabbitMQ` returns nil, and `runCleanupCycle` reports counters that
  remain zero. Failed-queue cleanup can delete messages, so changes need
  focused tests before behavior changes.
- **Some cleanup tests do not exercise the implementation:** for example,
  `TestCleanupFailedQueue_ContextCancellation` only logs, while the summary and
  age tests reproduce small calculations rather than checking cleanup
  behavior. Add test seams around the broker operations rather than requiring
  a live RabbitMQ instance.
- **Repeated delivery mechanics:** worker modules repeat retry, failed-queue,
  status, and acknowledgement handling. This is a refactoring opportunity,
  but the handoff ordering and bounded retry policy are contracts; establish
  behavior tests before extracting shared logic.
- **Shutdown/cancellation is uneven:** several workers create job timeouts from
  `context.Background()`. Review each long-running worker's signal handling and
  delivery lifecycle before changing cancellation or acknowledgement behavior.

### Architectural decisions, not first-week fixes

- **Status is process-local:** architecture documentation states that uploader
  progress snapshots are lost on restart and are not shared across replicas.
  This needs a persistence/fan-out decision, not an opportunistic store change.
- **Retention coverage is incomplete by design/documentation:** datacleaner
  removes original uploads and redacted text, while extracted text, skills,
  offer, and AI artifacts need separate lifecycle policies. Agree a retention
  matrix before expanding deletion behavior.
- **Deployment boundary:** some APIs document that they have no authentication
  or ownership checks. Keep them behind the access-controlled application
  boundary; authentication design is outside the junior's initial refactoring
  tickets.

## Recommended backlog

Order is intentional; independent items can be picked separately, while
contract-sensitive work follows its prerequisite tests.

1. **Wire each metrics server to its service port (easy).** Replace the copied
   AIweighter environment-variable name in the six affected entry points with
   the corresponding service-specific name. Verify configured ports with a
   focused startup or configuration check and confirm `/metrics` is reachable
   on the selected port.
2. **Remove the tracked roleinsights executable (easy).** Check repository
   build/deployment references first; if it is not a required release artifact,
   delete it from version control and add/adjust the narrow ignore rule.
   Confirm the module still builds from source.
3. **Replace placeholder datacleaner tests (easy-to-medium).** Add unit tests
   that execute cleanup logic for old, recent, and timestamp-less messages,
   dry-run behavior, cancellation, and broker-operation errors. Use a small
   test seam for queue operations; do not require a live broker.
4. **Make datacleaner reporting and queue coverage truthful (medium; after 3).**
   Remove the stale TODO, reconcile the queue list with documented failed
   queues (including role insights), and report per-queue failures and actual
   deletion/purge counts. Preserve dry-run and age semantics. Explicitly test
   that errors are visible and that cleanup does not accidentally operate on
   unintended queues.
5. **Lock down core pipeline handoff behavior (medium).** Add focused tests for
   extractor → piistripper → skillmatcher transitions: output is persisted
   before confirmed publication, acknowledgement follows confirmation, retry
   counts are bounded, and exhausted or malformed jobs reach the correct
   failed queue. Cover publish/confirm failure paths without changing behavior.
6. **Review worker shutdown and cancellation (medium; after 5).** For each
   consumer, define and test what happens on SIGTERM/SIGINT: stop accepting
   work, cancel bounded in-flight operations where safe, and leave unacknowledged
   deliveries available for at-least-once redelivery. Avoid acknowledging a
   job whose handoff was not confirmed.
7. **Refactor repeated worker delivery mechanics selectively (medium; after 5).**
   Compare the six worker implementations and extract only mechanics proven
   identical by tests. Keep service-specific job validation, status messages,
   result persistence, and retry policy explicit. Test affected modules and
   preserve handoff contracts.
8. **Decide and document a complete artifact-retention matrix (architecture
   spike).** Enumerate uploads, extracted/redacted text, scores, offer text,
   generated outputs, and failed messages; select the enforcing layer and
   retention period for each with the data-policy owner. Implement deletion
   only after that decision, with dry-run and boundary tests.
9. **Design durable status recovery/fan-out (architecture spike).** Propose
   options and an ADR for status surviving uploader restarts and reaching
   multiple replicas. Define client-visible recovery and terminal-state
   behavior; do not choose a storage or broker design until deployment owners
   agree.

## Working and validation notes

- Each service is a separate Go module. Run focused checks from that module,
  for example `go -C services/piistripper test ./...`; do not assume the repo
  root is a Go module.
- Changes to `services/common` require testing its consumers as well as
  `services/common`. Cross-service handoff changes warrant `sh test.sh` after
  focused module tests.
- Keep backlog tickets small. For behavior changes, first state the invariant,
  add a failing test, then implement the smallest fix and test both success and
  failure paths.
