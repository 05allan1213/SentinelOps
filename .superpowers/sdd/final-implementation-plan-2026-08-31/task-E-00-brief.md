# Task E-00 brief (coordinator)

Task card: read `output/final-implementation-plan-2026-08-31.md` lines 1494-1532 (section `### Task E-00: Run gpt-taste visual direction gate for the Runtime Operate surface`) in full; it is the authoritative scope.

## Environment

- Work in `/home/monody/project/.worktrees/sentinelops-e` (branch `feat/phase-e-20260910`, base `f461a3c`). Do NOT edit `/home/monody/project/SentinelOps`.
- `grill-truth.md` and the plan are copied into this worktree root/output. `web/node_modules` is installed (`npm ci` done).
- Node 24.19.0, npm 12.0.2. Desktop viewports only: 1280 and 1440. Mobile is `OUT OF SCOPE`.

## Coordinator instructions

1. This is a design gate. It creates documentation only. Do not modify any product code, API, migration, route, dependency, test, or `web/` file. `git status` after your commit must show no product-code change from you.
2. Use the `gpt-taste` skill: read `/home/monody/.agents/skills/gpt-taste/SKILL.md` completely, then apply it in **Operate mode** as required by the task card. Include its mandatory deterministic Python-style selection record (mock Python RNG output) inside the artifact's pre-flight section, and then explicitly override every AIDA / hero / bento / GSAP / randomized-layout / new-font / new-palette recommendation that conflicts with the frozen SentinelOps console direction. Record the override table so a reviewer can see exactly what was rejected and why.
3. Inventory the current console before writing the direction. Read at minimum: `web/src/components/layout/Sidebar.tsx`, `web/src/components/layout/Layout.tsx`, `web/src/assets/styles/index.css`, `web/src/pages/chat/`, `web/src/pages/event-analysis/`, `web/src/components/common/*.tsx`, `web/src/index.css`, `web/src/App.tsx`, and the frozen Runtime contract in `grill-truth.md` (sections 4, 5.4.2, 6, 8 unit E, 9) plus plan sections `Global Constraints`, `Frozen Public Contract`, and tasks E-01..E-07. Cite concrete file/line anchors for every claim about the current UI.
4. The direction must be reviewable for every Runtime surface: global navigation and Agent Runtime group; Runs list (filters, table density, states); Run Detail shell and each tab (overview, timeline, attempts, effects, evidence, context, trace); Recovery dialog + operation progress; Capabilities; Safety; Worker Health with Eval/Release/Retention read-only sections; and the Chat reading behavior that shares the shell.
5. Required direction fields (a missing field is a gate failure): information architecture; hierarchy; density; typography; spacing; observability-console language; Chat reading behavior; status semantics; Timeline/Tabs/table/detail usage; all loading/empty/partial/unavailable/failed states; Recovery interaction hierarchy.
6. Frozen constraints to encode: dark Sidebar, indigo primary, existing semantic status colors, Tailwind 4 CSS variables and `cn()`, existing primitives (`DataBlock`, `StatCard`, `Pagination`, `ConfirmDialog`, `CustomSelect`), Lucide icons, Framer Motion only; transitions 150-250ms; `prefers-reduced-motion` respected; no GSAP; no Neon/Cyberpunk/Card Soup; no second design system/shadcn init; only `succeeded` renders as success and parked/reconciling/unknown/unavailable/not_run must be visually distinct; no page-level horizontal overflow at 1280/1440.
7. Write the two required artifacts: `docs/implementation/runtime-visual-direction-2026-08-31.md` (create the directory if needed) and `output/runtime-visual-gate-2026-08-31.md`. The gate artifact records the checklist run, the reviewer-facing acceptance criteria per surface, the current-state inventory, and explicit `PASS`/`FAIL`/`NOT RUN` labels.
8. Commit only these files with the exact message `docs(runtime): record visual direction gate`. Do not push.
9. Report to the coordinator: `.superpowers/sdd/final-implementation-plan-2026-08-31/task-E-00-report.md` containing status, files, the deterministic selection record, override decisions, self-check results, and the final command/verification table. Mark anything not executed as `NOT RUN`.

## Coordinator rulings

- The gpt-taste AIDA/hero/bento/GSAP/random-font/random-layout mandates are intentionally overridden by the frozen product direction (plan Global Constraints and grill-truth section 4). Overriding them is required, not optional.
- `docs/implementation/` does not exist yet in this worktree; creating it is in scope for this task.
- If the skill demands imagery/assets that the product does not have, record the absence as an explicit bounded decision instead of inventing assets.

## Stop conditions

Stop and report if the task would require changing product code, routes, DTO/API contracts, dependencies, or if any required direction field cannot be decided from the frozen sources.
