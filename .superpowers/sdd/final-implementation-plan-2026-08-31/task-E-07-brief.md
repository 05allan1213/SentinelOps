# Task E-07 brief (coordinator)

Task card: read `output/final-implementation-plan-2026-08-31.md` lines 1801-1848 (section `### Task E-07: Add Capabilities, Safety, Worker Health and conditional Eval/Release views`) in full; it is the authoritative scope.

## Environment

- Work in `/home/monody/project/.worktrees/sentinelops-e` (branch `feat/phase-e-20260910`). Do NOT edit `/home/monody/project/SentinelOps`.
- Prerequisites already committed: E-00 direction, E-01 data layer, E-03 Runs page and the `Agent Runtime` navigation group (which already links to `Capabilities`, `Safety`, `Worker Health`), plus backend H-01/H-02 read models. Read them first.
- Commands from `web/`: `npm run test:unit`, `npm run lint`, `npm run build`, `npx playwright test tests/ui/runtime-capabilities.spec.ts --project=chromium`. Desktop 1280/1440 only.

## Scope

- Create `web/src/pages/runtime/capabilities.tsx`, `web/src/pages/runtime/safety.tsx`, `web/src/pages/runtime/worker-health.tsx`, and `web/src/pages/runtime/components/{CapabilityTable,SafetySummary,WorkerHealthTable,EvalReleaseSummary}.tsx`.
- Register the three routes `/runtime/capabilities`, `/runtime/safety`, `/runtime/worker-health` in `web/src/App.tsx` (E-03 added only the navigation links and stated the routes are owned here). Do not add a fourth `Status` navigation item and do not relabel anything else.
- Test: `web/tests/ui/runtime-capabilities.spec.ts`.
- Do not create Run pages/panels (E-03..E-06 own them), Go code, dependencies, a second client/store, or F work.

## Interfaces (frozen)

- Capability table renders `source/configured_state/observed_worker_state/risk/revision/schema_hash/required_gate/allowed_agents/last_observed_at/ResourceMeta`.
- Safety summary renders static/dynamic/effective gates, shadow state, L1/L2 effective state, policy/catalog revision and audit availability.
- Worker health renders persisted worker rows, heartbeat/stale reason, runtime compatibility, active Run/generation and nullable aggregate counts.
- `EvalReleaseSummary` renders as read-only sections on `/runtime/worker-health` consuming the optional Eval/Release/Retention hooks exactly as returned.
- Reuse only the E-01 hooks `useRuntimeCapabilities`, `useRuntimeSafety`, `useRuntimeWorkerHealth`, `useRuntimeEval`, `useRuntimeRelease`, `useRuntimeRetention` and existing Sidebar/CSS tokens.

## Coordinator instructions

1. Read-only only. Never add a capability toggle, Skill editor, MCP secret form, release control, rollback button or database management action.
2. Separate configured and observed columns visually and semantically. A missing observation shows `unavailable`/`not_observed`; never synthesise "loaded", "healthy" or a worker count.
3. Safety shows shadow mode as the reason effective writes are closed and distinguishes configured caps from current effective caps. Gate changes stay on the existing settings page/API.
4. Keep Eval/Release/Retention on the Worker Health route so the frozen navigation does not grow a second status centre. Label existing RAG Eval separately from Agent Eval. When the H-03 source is absent show `not_run`; Release gray/rollback without a durable source shows unavailable/`not_observed`; Retention shows policy and absent cleanup evidence explicitly.
5. High but scannable desktop density, stable columns, local overflow only. Link Run/Trace details through existing routes instead of duplicating views. No page-level horizontal overflow at 1280/1440.
6. TDD: write `runtime-capabilities.spec.ts` first, run it before implementation, and record the expected missing-route failure verbatim. Required assertions: configured/observed separation, `not_observed`/`unavailable`, shadow/effective gates, catalog metadata, worker stale/missing states, RAG-vs-Agent Eval labelling, Release/Retention `not_run`, and the absence of any write control.
7. Verify: focused spec at both viewports, full `npm run test:unit`, `npm run lint`, `npm run build` (68 warnings + large chunk are baseline), plus the existing smoke UI suite. Also confirm every Agent Runtime navigation link added by E-03 now resolves. Fix failures before committing.
8. Commit only your files with the exact message `feat(web-runtime): add capabilities safety and worker health views`. No push. On `index.lock`, wait and retry.
9. Write `.superpowers/sdd/final-implementation-plan-2026-08-31/task-E-07-report.md` with status, files, RED/GREEN evidence, the configured/observed and not_run/unavailable mapping tables, and the command/PASS-FAIL-NOT RUN evidence.

## Stop conditions

Stop and report if a required field is absent from the frozen DTO, if rendering the page would require inventing provider/Eval/Release facts, or if registering the routes would force a change to an unrelated existing route.
