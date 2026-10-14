---
name: go-code-review
description: Review Go changes in this repository for actionable correctness, reliability, and contract issues without making edits.
---

# Go code review

Use this skill to review Go diffs in `services/`. Review the changed code and its relevant callers, tests, and contracts; do not turn the review into a general rewrite or report style preferences as defects.

## Review priorities

- Look for incorrect behavior, lost or duplicated work, unsafe concurrency, resource leaks, ignored errors, and missing cancellation or timeouts on blocking operations.
- Check input validation, boundary conditions, and whether tests exercise both the intended behavior and meaningful failure paths.
- For queue and storage code, verify acknowledgement order, publish confirmation, retry behavior, deterministic keys, and consistency with the message and object contracts.
- Check changes to shared code against each affected service and its module-specific tests.
- Treat architecture documentation in `docs/cv-analyzer/modules/architecture/` as authoritative for workflow and queue semantics.

## Report

Report only actionable findings. Order them by severity, cite the narrowest relevant file and line range, explain the concrete impact, and give a concise remediation direction. If no material issues are found, say so. Do not edit files unless separately asked to implement fixes.
