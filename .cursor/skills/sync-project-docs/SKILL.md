---
name: sync-project-docs
description: >-
  Keeps DockLab project documentation in sync with the codebase. Requires
  reading and updating README.md when implementing features, APIs,
  infrastructure, or product behavior changes. Use when adding or changing
  product behavior or before finishing any feature implementation task.
---

# Sync DockLab Project Docs

When implementing features in DockLab, **always** reconcile the codebase with [README.md](../../README.md) before considering the task done.

Do not skip this step. Doc updates are part of the feature, not optional follow-up.

---

## When to apply

Apply this skill whenever work touches:

- Backend handlers, services, routes, or schema
- Frontend pages, flows, or dashboard views
- Terraform, Docker, CI/CD, or env configuration
- Background workers, lifecycle policies, or reconciliation

Skip only for pure refactors with **zero** user-visible or operational change. Even then, verify docs still match behavior.

---

## Workflow

```text
Doc sync:
- [ ] Read README.md
- [ ] Identify what changed in code vs what docs claim
- [ ] Update README.md (if user-facing behavior changed)
- [ ] Confirm known limitations and future improvements are still accurate
```

### Update README.md when:

- New/changed API endpoints, env vars, or CLI commands
- New dashboard views, user flows, or prerequisites
- Changed local dev, validation, or end-to-end test steps
- New known limitations or resolved limitations
- CI/CD or Terraform setup changes

Keep README practical: setup, usage, API list, limitations table, test checklist, future improvements.

### Maintain viability honesty

README must reflect:

1. **What works today** — accurate, not aspirational
2. **What is still missing** — in the Known limitations table and Future improvements section
3. **What comes next** — aligned with known gaps, not a separate planning doc

If a feature closes a gap, remove or downgrade it from Known limitations.

---

## Quality bar

Doc updates must be **accurate**, **readable**, and **honest about gaps** — never mark limitations resolved until the code actually delivers the behavior.
