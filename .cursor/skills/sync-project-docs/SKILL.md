---
name: sync-project-docs
description: >-
  Keeps DockLab project documentation in sync with the codebase. Requires
  reading and updating README.md, plan/docklab_project_plan.md, and
  plan/sprints.md when implementing features, APIs, infrastructure, or sprint
  work. Use when adding or changing product behavior, completing sprint
  scope, or before finishing any feature implementation task.
---

# Sync DockLab Project Docs

When implementing features in DockLab, **always** reconcile the codebase with these three files before considering the task done:

| File | Role |
|------|------|
| [README.md](../../README.md) | User-facing: setup, API routes, features, env vars, limitations, test checklist |
| [plan/docklab_project_plan.md](../../plan/docklab_project_plan.md) | Long-term plan: phase status, architecture, implemented vs remaining gaps |
| [plan/sprints.md](../../plan/sprints.md) | Sprint tracking: delivered scope, definition of done, next sprint priorities |

Do not skip this step. Doc updates are part of the feature, not optional follow-up.

---

## When to apply

Apply this skill whenever work touches:

- Backend handlers, services, routes, or schema
- Frontend pages, flows, or dashboard views
- Terraform, Docker, CI/CD, or env configuration
- Background workers, lifecycle policies, or reconciliation
- Sprint completion or partial sprint delivery

Skip only for pure refactors with **zero** user-visible or operational change. Even then, verify docs still match behavior.

---

## Workflow

Copy this checklist and complete every item before finishing:

```text
Doc sync:
- [ ] Read README.md, plan/docklab_project_plan.md, and plan/sprints.md
- [ ] Identify what changed in code vs what docs claim
- [ ] Update README.md (if user-facing behavior changed)
- [ ] Update plan/docklab_project_plan.md (if phase/status/architecture changed)
- [ ] Update plan/sprints.md (if sprint scope or status changed)
- [ ] Confirm docs still list remaining work needed for viability
- [ ] Confirm "Last updated" / current status dates are accurate
```

### Step 1: Read all three docs

Read every file fully (or the relevant sections if the change is narrow). Do not update from memory.

### Step 2: Compare code to docs

Ask:

- Do documented API routes, env vars, and features match the implementation?
- Are limitations and known gaps still accurate?
- Is sprint/phase status honest (not "in progress" when done, not "done" when partial)?
- Does the roadmap still reflect the highest-priority remaining work?

### Step 3: Update each file by responsibility

**README.md** — update when:

- New/changed API endpoints, env vars, or CLI commands
- New dashboard views, user flows, or prerequisites
- Changed local dev or validation steps
- New known limitations or resolved limitations
- CI/CD or Terraform setup changes

Keep README practical: setup, usage, API list, limitations table, test checklist, roadmap links.

**plan/docklab_project_plan.md** — update when:

- A phase moves between not started / partial / complete
- Architecture diagram or tech stack choices change
- Implemented vs not-implemented tables need revision
- Viability gaps or current focus shift
- Engineering challenges are solved or new ones emerge

Keep the plan strategic: phase status, architecture, gaps, long-term scope.

**plan/sprints.md** — update when:

- Sprint scope is delivered, partially delivered, or reprioritized
- Definition of done is met or adjusted
- A new sprint is added or reordered
- Remaining scope within a sprint changes

Keep sprints tactical: per-sprint delivered/remaining scope, status markers, summary table.

### Step 4: Maintain viability honesty

All three docs must continue to reflect:

1. **What works today** — accurate, not aspirational
2. **What is still missing** — especially gaps that block a viable product (e.g. remote orchestration, cloud idle cleanup, production deployment)
3. **What comes next** — aligned across README roadmap, plan current focus, and sprint "next" row

If a feature closes a gap, remove or downgrade it from "not implemented" and update the next priority.

---

## Status markers

Use consistent markers in sprint/plan docs:

| Marker | Meaning |
|--------|---------|
| ✅ / Complete | Definition of done met |
| 🟡 / Partial | Some scope delivered; remaining listed explicitly |
| 🔲 / Not started | No meaningful implementation yet |

When completing sprint scope, move delivered items from "Scope" to "Delivered" and update the summary table.

---

## Minimal update examples

**Added API endpoint** `POST /api/v1/environments/:id/restart`:

- README: add to API list and test checklist if user-facing
- plan: note under relevant phase if it advances phase scope
- sprints: add under current sprint "Delivered" if in active sprint

**Completed Sprint 7 remote orchestration**:

- README: update status, limitations table (remove "local only" if fixed), usage steps
- plan: Phase 6 → complete; update architecture diagram; revise "not implemented"
- sprints: Sprint 7 → ✅; adjust summary table; set Sprint 8 as next

**Internal refactor only** (no behavior change):

- Quick read of all three docs; update only if something was already wrong

---

## Quality bar

Doc updates must be:

- **Accurate** — match the code that was just written
- **Readable** — short sections, tables where helpful, no stale copy from old sprints
- **Complete** — each file contains what its audience needs; cross-link between them instead of duplicating long prose
- **Honest about gaps** — never mark the project "viable" until remote workspaces, cloud lifecycle, and deployment gaps are actually closed

If unsure which file to update, update all three with minimal scoped edits rather than skipping one.
