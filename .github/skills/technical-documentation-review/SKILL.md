---
name: technical-documentation-review
description: Review repository documentation for technical accuracy, consistency, and usefulness against the implementation and documented contracts.
---

# Technical documentation review

Use this skill to review documentation changes or investigate an existing technical document. Report issues; do not rewrite or edit documentation unless asked.

## Review approach

- Verify claims against the implementation and authoritative sources rather than assuming the prose is correct. For workflow and queue semantics, architecture documentation under `docs/cv-analyzer/modules/architecture/` is authoritative; flag material contradictions between it and runtime behavior.
- Check commands, paths, configuration names, defaults, message shapes, object keys, service responsibilities, and failure behavior against the relevant code and service docs.
- For Antora content, check component/module placement, xref targets, resource IDs, and AsciiDoc structure where relevant.
- Look for missing prerequisites, ambiguous steps, stale examples, and claims that could mislead an operator or contributor. Do not report wording preferences unless they obscure meaning.
- Distinguish confirmed inaccuracies from claims that could not be verified. Do not invent expected behavior to fill documentation gaps.

## Report

List only actionable findings, ordered by impact. For each, cite the file and narrowest relevant line range, explain why it matters, and state what should be corrected or verified. If no material issues are found, say so. Do not make changes unless separately requested.
