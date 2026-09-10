# Task E-04 brief (coordinator)

Task card: read `output/final-implementation-plan-2026-08-31.md` lines 1666-1710 (section `### Task E-04: Build Run Detail shell with Overview, Timeline and server-state panels`) in full; it is the authoritative scope.

## Environment

- Work in `/home/monody/project/.worktrees/sentinelops-e` (branch `feat/phase-e-20260910`). Do NOT edit `/home/monody/project/SentinelOps`.
- Prerequisites already committed: E-00 visual direction (`docs/implementation/runtime-visual-direction-2026-08-31.md`), E-01 data layer (`web/src/types|services/runtime.ts`, `web/src/hooks/useRuntimeQueries.ts`), E-02 tail (`web/src/hooks/useRuntimeEventTail.ts`), E-03 Runs page + `Agent Runtime` navigation. Read all of them before writing code, and follow the E-00 direction for hierarchy/density/spacing.
- Commands from `web/`: `npm run test:unit`, `npm run lint`, `npm run build`, `npx playwright test tests/ui/<spec> --project=chromium`. Desktop viewports 1280 and 1440 only; mobile is `OUT OF SCOPE`.

## Scope

- Create `web/src/pages/runtime/detail.tsx`, `web/src/pages/runtime/components/RunDetailTabs.tsx`, `RunOverview.tsx`, `RunTimeline.tsx`, `RuntimeQualityState.tsx`.
- Test: `web/tests/ui/runtime-detail-overview.spec.ts`.
- Bounded allowance: this task owns tab wiring. Register the `/runtime/runs/:runId` route (co-located with the E-03 Runtime route registration; keep every existing route and the E-03 `/runtime/runs` route intact) and render all seven tabs. The `attempts|effects|evidence|context|trace` tabs may render an explicit anchored placeholder in this task; E-05/E-06 replace those bodies. No other product file may change.
- Do not create the E-05/E-06 panels, the E-07 pages, Go code, dependencies, a second client/store/event bus, or F work.

## Interfaces (frozen)

- Route `/runtime/runs/:runId` with tabs `overview|timeline|attempts|effects|evidence|context|trace`; the active tab is URL search state, never server state and never Zustand server state.
- `RunOverview` consumes `RunDetailDTO` and renders Status, Current Phase, Agent/orchestration, Session, Worker/lease/heartbeat/generation, Runtime compatibility, Gate summary, Budget and quality metadata.
- `RunTimeline` consumes `TimelineRes` and renders every `RuntimeEventDTO` with seq/time/type/Attempt/generation/trace/operation; grouping/collapse is display-only.
- Reuse `useRuntimeRun`, `useRuntimeTimeline`, `useRuntimeEventTail`, `RuntimeStatusBadge` (from E-03), `DataBlock`, `StatCard`, `cn()`.

## Coordinator instructions

1. Load Run detail and start the E-02 tail only for a valid Run ID whose server status is non-terminal. Use per-panel error boundaries so one failing child leaves the proven Run identity visible.
2. Render `availability`, `data_quality`, `reason_code` and `not_run` as explicit text through `RuntimeQualityState`. Do not derive phase from event names, Worker active from timestamps, compatibility from a single hash, or success from local loading state.
3. Dispatch on the canonical status; only `succeeded` may use success styling. `parked`, `reconciling`, `retryable_failed`, `canceled`, `waiting_approval`, `pending`, `running` and unknown values stay visually distinct.
4. Show lease owner only as the server-redacted Worker ID, plus generation, heartbeat/expiry and reason. Show `configured` vs `observed` and `run`/`checkpoint`/`worker` compatibility separately.
5. Timeline keeps every persisted canonical Event in seq order (plan/execute/replan, AgentTool, Tool, Approval, Checkpoint, Effect, Budget, Evidence, Trace). Collapsed-by-default payload; use the shared `MarkdownRenderer` only for bounded summaries. Collapse/filter must never delete an event from the underlying list.
6. Tabs are keyboard reachable with semantic buttons/ARIA; tab content has stable min/max dimensions and local scrolling for long data. No page-level horizontal overflow at 1280/1440.
7. TDD: write `runtime-detail-overview.spec.ts` first, run it before implementation, and record the expected missing-route/shell failure verbatim. Required assertions: all tabs present, loading/empty/unavailable/partial/failed panel states, canonical status/phase, lease/generation, quality metadata, full Event retention, operation correlation, no page overflow. Controlled API responses only; real Runtime/Worker execution stays `NOT RUN`.
8. Verify: focused spec at both viewports, full `npm run test:unit`, `npm run lint`, `npm run build` (68 pre-existing warnings + large-chunk warning are baseline), plus the existing smoke/approval/markdown suites to prove no regression elsewhere. Fix failures before committing.
9. Commit only your files with the exact message `feat(web-runtime): add run detail overview and timeline`. No push. On `index.lock`, wait and retry.
10. Write `.superpowers/sdd/final-implementation-plan-2026-08-31/task-E-04-report.md` with status, files, RED/GREEN browser evidence, the tab/status/quality mapping tables, and the command/PASS-FAIL-NOT RUN evidence.

## Stop conditions

Stop and report if E-01/E-02 cannot express a required Run/Event field or panel state, if a tab cannot be registered without rewriting an existing route, or if evidence would require presenting controlled fixtures as real Runtime execution.
