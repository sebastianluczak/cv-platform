---
name: go-service-development
description: Implement or modify Go code in this repository's services using its module boundaries, Go conventions, and validation commands.
---

# Go service development

Use this skill when changing Go code under `services/`. Keep changes focused on the requested behavior; prefer the standard library and existing patterns over new dependencies or abstractions.

## Repository conventions

- Each service under `services/` is its own Go module. Run Go commands from the affected module, for example `go -C services/uploader test ./...`; do not assume the repository root is a Go module.
- `services/common` is a shared module. Preserve its consumers' contracts and test affected modules when changing shared code.
- Format changed Go files with `gofmt`.
- Keep errors explicit and useful. Wrap errors with operation context when returning them; do not silently discard failures or add broad recovery behavior.
- Pass `context.Context` through operations that can block on network, storage, or queue work. Avoid introducing interfaces, configuration, or dependencies without a concrete need.
- Add or update focused tests for behavior changes, including relevant failure paths. Run the affected module's tests; use `sh test.sh` for the complete backend suite when cross-service changes warrant it.

## Before finishing

Check that the change stays within the affected module boundaries, is formatted, and preserves existing service contracts. Update directly related architecture or operational documentation when behavior or contracts change.
